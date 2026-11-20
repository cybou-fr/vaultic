package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/restic/restic/internal/backend/all"
	"github.com/restic/restic/internal/backend/azure"
	"github.com/restic/restic/internal/backend/b2"
	"github.com/restic/restic/internal/backend/gs"
	"github.com/restic/restic/internal/backend/local"
	"github.com/restic/restic/internal/backend/location"
	"github.com/restic/restic/internal/backend/rclone"
	"github.com/restic/restic/internal/backend/rest"
	"github.com/restic/restic/internal/backend/s3"
	"github.com/restic/restic/internal/backend/sftp"
	"github.com/restic/restic/internal/backend/swift"
)

// vaulticGuidedStorageForm contains only non-secret repository location data.
// Credentials and the restic encryption password are supplied separately.
type vaulticGuidedStorageForm struct {
	Scheme   string
	Path     string
	Host     string
	Port     string
	Username string
	Bucket   string
	Prefix   string
	Remote   string
}

var (
	errGuidedField = errors.New("Champ trop long ou contenant des caractères non autorisés.")
	sftpUserRe     = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,63}$`)
	sftpHostRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,252}$`)
	rcloneRemoteRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.+-]{0,63}$`)
	gcpProjectRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:._-]{0,127}$`)
)

func vaulticGuidedStorageField(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) > 512 || strings.ContainsAny(value, "\x00\r\n?#") {
		return "", errGuidedField
	}
	return value, nil
}

func vaulticGuidedStoragePrefix(raw string) (string, error) {
	p, err := vaulticGuidedStorageField(raw)
	if err != nil {
		return "", err
	}
	p = strings.Trim(p, "/")
	if strings.Contains(p, "\\") {
		return "", errors.New("Utilisez des barres obliques dans le chemin distant.")
	}
	if p != "" {
		for _, segment := range strings.Split(p, "/") {
			if segment == "" || segment == "." || segment == ".." {
				return "", errors.New("Chemin distant invalide.")
			}
		}
	}
	return p, nil
}

func vaulticGuidedBucket(raw string) (string, error) {
	bucket, err := vaulticGuidedStorageField(raw)
	if err != nil {
		return "", err
	}
	if bucket == "" || strings.HasPrefix(bucket, "-") || strings.ContainsAny(bucket, ":/@\\ \t") {
		return "", errors.New("Indiquez un bucket ou conteneur valide, sans espace ni barre oblique.")
	}
	return bucket, nil
}

// vaulticGuidedHost accepts a DNS name, an IPv4 address or a bracketed IPv6
// address. A leading dash is refused: ssh would read it as an option.
func vaulticGuidedHost(raw string) (host string, bracketed bool, err error) {
	host, err = vaulticGuidedStorageField(raw)
	if err != nil {
		return "", false, err
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		ip := net.ParseIP(host[1 : len(host)-1])
		if ip == nil || ip.To4() != nil {
			return "", false, errors.New("Adresse IPv6 invalide.")
		}
		return host, true, nil
	}
	if !sftpHostRe.MatchString(host) {
		return "", false, errors.New("Nom d'hôte invalide (lettres, chiffres, point et tiret uniquement).")
	}
	return host, false, nil
}

func vaulticGuidedPort(raw string) (string, error) {
	port, err := vaulticGuidedStorageField(raw)
	if err != nil || port == "" {
		return "", err
	}
	n, convErr := strconv.Atoi(port)
	if convErr != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
		return "", errors.New("Port SSH invalide (1 à 65535).")
	}
	return port, nil
}

func vaulticGuidedSFTPPath(raw string) (string, error) {
	p, err := vaulticGuidedStorageField(raw)
	if err != nil {
		return "", err
	}
	if p == "" || p == "/" || strings.Contains(p, "\\") || strings.HasPrefix(p, "~") {
		return "", errors.New("Indiquez un dossier distant (absolu ou relatif au répertoire personnel, sans ~).")
	}
	for _, segment := range strings.Split(strings.TrimPrefix(p, "/"), "/") {
		if segment == ".." {
			return "", errors.New("Chemin distant invalide.")
		}
	}
	return p, nil
}

// vaulticGuidedRESTURL accepts HTTPS. Plain HTTP is only accepted for a loopback
// server, where nothing travels over a network.
func vaulticGuidedRESTURL(raw string) (*url.URL, error) {
	raw, err := vaulticGuidedStorageField(raw)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(raw)
	bad := errors.New("Indiquez une URL HTTPS sans identifiants ni paramètres.")
	if err != nil || u == nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.Contains(raw, "@") {
		return nil, bad
	}
	switch u.Scheme {
	case "https":
	case "http":
		ip := net.ParseIP(u.Hostname())
		if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil, errors.New("HTTP n'est autorisé que vers localhost ; utilisez HTTPS pour un serveur distant.")
		}
	default:
		return nil, bad
	}
	return u, nil
}

func vaulticGuidedRemote(raw string) (string, error) {
	remote, err := vaulticGuidedStorageField(raw)
	if err != nil {
		return "", err
	}
	if !rcloneRemoteRe.MatchString(remote) {
		return "", errors.New("Nom de remote rclone invalide (lettres, chiffres, _ . + -).")
	}
	return remote, nil
}

func vaulticBuildGuidedStorageAddress(form vaulticGuidedStorageForm) (string, error) {
	var address string
	switch form.Scheme {
	case "local":
		p, err := vaulticGuidedStorageField(form.Path)
		if err != nil {
			return "", err
		}
		if p == "" {
			return "", errors.New("Choisissez un dossier de dépôt.")
		}
		if strings.Contains(p, "://") {
			return "", errors.New("Chemin local invalide.")
		}
		address = "local:" + p
	case "rest":
		u, err := vaulticGuidedRESTURL(form.Path)
		if err != nil {
			return "", err
		}
		address = "rest:" + u.String()
	case "sftp":
		user, err := vaulticGuidedStorageField(form.Username)
		if err != nil {
			return "", err
		}
		if !sftpUserRe.MatchString(user) {
			return "", errors.New("Nom d'utilisateur SSH invalide.")
		}
		host, bracketed, err := vaulticGuidedHost(form.Host)
		if err != nil {
			return "", err
		}
		port, err := vaulticGuidedPort(form.Port)
		if err != nil {
			return "", err
		}
		p, err := vaulticGuidedSFTPPath(form.Path)
		if err != nil {
			return "", err
		}
		if port == "" && !bracketed {
			address = fmt.Sprintf("sftp:%s@%s:%s", user, host, p)
		} else {
			// Only the URL form of the restic parser carries a port.
			// The directory is url.Path[1:], hence "//" for an absolute path.
			hostPort := host
			if port != "" {
				hostPort += ":" + port
			}
			address = fmt.Sprintf("sftp://%s@%s/%s", user, hostPort, p)
		}
	case "gs", "swift":
		bucket, err := vaulticGuidedBucket(form.Bucket)
		if err != nil {
			return "", err
		}
		prefix, err := vaulticGuidedStoragePrefix(form.Prefix)
		if err != nil {
			return "", err
		}
		address = form.Scheme + ":" + bucket + ":/" + prefix
	case "rclone":
		remote, err := vaulticGuidedRemote(form.Remote)
		if err != nil {
			return "", err
		}
		prefix, err := vaulticGuidedStoragePrefix(form.Prefix)
		if err != nil {
			return "", err
		}
		address = "rclone:" + remote + ":" + prefix
	default:
		return "", errors.New("Ce type de stockage n'est pas pris en charge par cet assistant.")
	}
	if err := vaulticValidateDepot(address); err != nil {
		return "", err
	}
	return address, nil
}

// ---------------------------------------------------------------------------
// Field specification: one table drives the dynamic form and its validation.

type vaulticStorageField struct {
	Key, Label, Placeholder, Default, Help string
	Required, Secret, Browse               bool
	Choices                                []string
}

const (
	vaulticAzureKeyChoice = "Clé de compte"
	vaulticAzureSASChoice = "Jeton SAS"
)

func vaulticStorageFields(scheme string) []vaulticStorageField {
	prefix := vaulticStorageField{Key: "prefix", Label: "Sous-dossier", Placeholder: "vaultic", Default: "vaultic", Help: "Sous-dossier du dépôt. Laissez vide pour la racine."}
	switch scheme {
	case "local":
		return []vaulticStorageField{{Key: "path", Label: "Dossier du dépôt", Placeholder: "D:\\Sauvegardes\\vaultic", Required: true, Browse: true}}
	case "sftp":
		return []vaulticStorageField{
			{Key: "host", Label: "Serveur SSH", Placeholder: "serveur.exemple.fr", Required: true},
			{Key: "port", Label: "Port SSH", Placeholder: "22 (par défaut)", Help: "Facultatif."},
			{Key: "user", Label: "Utilisateur", Placeholder: "backup", Required: true},
			{Key: "path", Label: "Dossier distant", Placeholder: "/srv/vaultic", Required: true, Help: "Authentification par clé SSH déjà configurée (ssh-agent)."},
		}
	case "rest":
		return []vaulticStorageField{
			{Key: "url", Label: "URL du serveur", Placeholder: "https://serveur.exemple.fr/depot", Required: true},
			{Key: "username", Label: "Utilisateur", Placeholder: "facultatif", Help: "Identifiants gardés en mémoire pour cette session seulement."},
			{Key: "secret", Label: "Mot de passe du serveur", Secret: true, Placeholder: "facultatif"},
		}
	case "gs":
		return []vaulticStorageField{
			{Key: "bucket", Label: "Bucket", Placeholder: "mon-bucket", Required: true}, prefix,
			{Key: "project", Label: "ID du projet", Placeholder: "facultatif, utile à la création", Help: "Authentification : Application Default Credentials de Google."},
		}
	case "swift":
		return []vaulticStorageField{
			{Key: "bucket", Label: "Conteneur", Placeholder: "mon-conteneur", Required: true}, prefix,
		}
	case "rclone":
		return []vaulticStorageField{
			{Key: "remote", Label: "Remote rclone", Placeholder: "cloud", Required: true, Help: "Nom d'un remote déjà configuré avec rclone config."},
			{Key: "prefix", Label: "Sous-dossier", Placeholder: "vaultic", Default: "vaultic"},
		}
	case "s3":
		return []vaulticStorageField{
			{Key: "endpoint", Label: "Endpoint", Placeholder: "s3.amazonaws.com", Default: "s3.amazonaws.com", Required: true},
			{Key: "bucket", Label: "Bucket", Placeholder: "mon-bucket", Required: true}, prefix,
			{Key: "region", Label: "Région", Placeholder: "eu-west-3 (facultatif)"},
			{Key: "access", Label: "Access Key ID", Placeholder: "facultatif", Help: "Clés gardées en mémoire. Vides : configuration système."},
			{Key: "secret", Label: "Secret Access Key", Secret: true},
		}
	case "b2":
		return []vaulticStorageField{
			{Key: "bucket", Label: "Bucket", Placeholder: "mon-bucket", Required: true}, prefix,
			{Key: "access", Label: "Account ID / Key ID", Placeholder: "facultatif", Help: "Clés gardées en mémoire. Vides : configuration système."},
			{Key: "secret", Label: "Application Key", Secret: true},
		}
	case "azure":
		return []vaulticStorageField{
			{Key: "account", Label: "Compte de stockage", Placeholder: "moncompte"},
			{Key: "bucket", Label: "Conteneur", Placeholder: "mon-conteneur", Required: true}, prefix,
			{Key: "authmode", Label: "Authentification", Choices: []string{vaulticAzureKeyChoice, vaulticAzureSASChoice}, Default: vaulticAzureKeyChoice},
			{Key: "secret", Label: "Clé ou SAS", Secret: true, Help: "Vide : configuration système (Azure CLI, identité managée…)."},
		}
	}
	return nil
}

// vaulticValidateStorageField validates one field in isolation (inline hints).
func vaulticValidateStorageField(scheme, key, value string) error {
	empty := strings.TrimSpace(value) == ""
	for _, f := range vaulticStorageFields(scheme) {
		if f.Key == key && f.Required && empty {
			return errors.New("Champ obligatoire.")
		}
	}
	if empty {
		return nil
	}
	var err error
	switch {
	case scheme == "local" && key == "path":
		_, err = vaulticBuildGuidedStorageAddress(vaulticGuidedStorageForm{Scheme: "local", Path: value})
	case scheme == "sftp" && key == "host":
		_, _, err = vaulticGuidedHost(value)
	case scheme == "sftp" && key == "port":
		_, err = vaulticGuidedPort(value)
	case scheme == "sftp" && key == "user":
		if v, e := vaulticGuidedStorageField(value); e != nil || !sftpUserRe.MatchString(v) {
			err = errors.New("Nom d'utilisateur SSH invalide.")
		}
	case scheme == "sftp" && key == "path":
		_, err = vaulticGuidedSFTPPath(value)
	case scheme == "rest" && key == "url":
		_, err = vaulticGuidedRESTURL(value)
	case scheme == "rclone" && key == "remote":
		_, err = vaulticGuidedRemote(value)
	case key == "prefix":
		_, err = vaulticGuidedStoragePrefix(value)
	case key == "bucket":
		_, err = vaulticGuidedBucket(value)
	case key == "project":
		if v, e := vaulticGuidedStorageField(value); e != nil || !gcpProjectRe.MatchString(v) {
			err = errors.New("Identifiant de projet invalide.")
		}
	case scheme == "s3" && key == "endpoint":
		_, err = vaulticCloudEndpoint(value)
	case key == "endpoint" || key == "access" || key == "account" || key == "region" || key == "username":
		_, err = vaulticCloudField(value)
	}
	return err
}

// vaulticStorageSetup is everything the connect/create flow needs. Auth and
// Password are secrets: they never go into the address or the settings file.
type vaulticStorageSetup struct {
	Name, Address, Password string
	Auth                    vaulticCloudAuth
}

// vaulticBuildStorageSetup turns the dynamic form values into an address plus
// per-repository session credentials. Keys: name, password and the field keys.
func vaulticBuildStorageSetup(scheme string, v map[string]string) (vaulticStorageSetup, error) {
	var out vaulticStorageSetup
	if _, ok := vaulticBackendByScheme(scheme); !ok {
		return out, errors.New("Choisissez un type de stockage.")
	}
	name, err := vaulticValidateSavedName(v["name"])
	if err != nil {
		return out, err
	}
	if v["password"] == "" {
		return out, errors.New("Saisissez le mot de passe du dépôt restic.")
	}
	switch scheme {
	case "s3", "b2", "azure":
		setup, err := vaulticBuildCloudSetup(vaulticCloudFormData{
			Scheme: scheme, Name: v["name"], Password: v["password"], Endpoint: v["endpoint"],
			Bucket: v["bucket"], Prefix: v["prefix"], Region: v["region"], Account: v["account"],
			AccessID: v["access"], Secret: v["secret"], AzureSAS: v["authmode"] == vaulticAzureSASChoice,
		})
		if err != nil {
			return out, err
		}
		return vaulticStorageSetup{Name: setup.Name, Address: setup.Address, Password: setup.Password, Auth: setup.Auth}, nil
	}
	form := vaulticGuidedStorageForm{
		Scheme: scheme, Path: v["path"], Host: v["host"], Port: v["port"], Username: v["user"],
		Bucket: v["bucket"], Prefix: v["prefix"], Remote: v["remote"],
	}
	if scheme == "rest" {
		form.Path = v["url"]
	}
	address, err := vaulticBuildGuidedStorageAddress(form)
	if err != nil {
		return out, err
	}
	out = vaulticStorageSetup{Name: name, Address: address, Password: v["password"]}
	switch scheme {
	case "rest":
		out.Auth = vaulticCloudAuth{Scheme: "rest", AccessID: strings.TrimSpace(v["username"]), Secret: v["secret"]}
	case "gs":
		out.Auth = vaulticCloudAuth{Scheme: "gs", ProjectID: strings.TrimSpace(v["project"])}
	}
	if out.Auth.Scheme != "" {
		if err := out.Auth.validate(); err != nil {
			return vaulticStorageSetup{}, err
		}
	}
	return out, nil
}

// vaulticParseStorageAddress maps a saved address back to form values using the
// engine's own parsers, so a repository can be edited in the guided form.
func vaulticParseStorageAddress(address string) (string, map[string]string, error) {
	loc, err := location.Parse(all.Backends(), strings.TrimSpace(address))
	if err != nil {
		return "", nil, err
	}
	v := map[string]string{}
	switch c := loc.Config.(type) {
	case *local.Config:
		v["path"] = c.Path
	case *sftp.Config:
		v["user"], v["host"], v["port"], v["path"] = c.User, c.Host, c.Port, c.Path
		if strings.Contains(c.Host, ":") {
			v["host"] = "[" + c.Host + "]"
		}
	case *rest.Config:
		u := *c.URL
		u.User = nil
		v["url"] = strings.TrimSuffix(u.String(), "/")
	case *gs.Config:
		v["bucket"], v["prefix"] = c.Bucket, c.Prefix
	case *swift.Config:
		v["bucket"], v["prefix"] = c.Container, c.Prefix
	case *rclone.Config:
		v["remote"], v["prefix"], _ = strings.Cut(c.Remote, ":")
	case *s3.Config:
		v["endpoint"], v["bucket"], v["prefix"] = c.Endpoint, c.Bucket, c.Prefix
	case *b2.Config:
		v["bucket"], v["prefix"] = c.Bucket, c.Prefix
	case *azure.Config:
		v["bucket"], v["prefix"] = c.Container, c.Prefix
	default:
		return "", nil, errors.New("Type de dépôt inconnu.")
	}
	return loc.Scheme, v, nil
}

// vaulticSafeLocation is the form shown in lists and logs: no password, query
// or fragment, whatever the stored value contains.
func vaulticSafeLocation(address string) string {
	address = strings.TrimSpace(address)
	scheme, rest, _ := strings.Cut(address, ":")
	target := ""
	switch {
	case scheme == "rest":
		target = rest
	case strings.HasPrefix(rest, "//"):
		target = address
	}
	if target != "" {
		if u, err := url.Parse(target); err == nil && u.Host != "" {
			if scheme == "sftp" && u.User != nil {
				u.User = url.User(u.User.Username())
			} else {
				u.User = nil
			}
			u.RawQuery, u.Fragment, u.RawFragment = "", "", ""
			if scheme == "rest" {
				return "rest:" + u.String()
			}
			return u.String()
		}
		return scheme + ":[adresse masquée]"
	}
	if i := strings.IndexAny(address, "?#"); i >= 0 {
		address = address[:i]
	}
	return address
}

// vaulticProviderName is the short provider label for an address.
func vaulticProviderName(address string) string {
	info, ok := vaulticBackendForAddress(address)
	if !ok {
		return "Inconnu"
	}
	label, _, _ := strings.Cut(info.Label, " — ")
	return label
}
