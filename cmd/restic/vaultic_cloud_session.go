package main

import (
    "context"
    "errors"
    "fmt"
    "net"
    "net/http"
    "net/url"
    "strings"
    "sync"

    "github.com/restic/restic/internal/backend"
    "github.com/restic/restic/internal/backend/all"
    "github.com/restic/restic/internal/backend/azure"
    "github.com/restic/restic/internal/backend/b2"
    "github.com/restic/restic/internal/backend/gs"
    "github.com/restic/restic/internal/backend/rest"
    "github.com/restic/restic/internal/backend/limiter"
    "github.com/restic/restic/internal/backend/location"
    "github.com/restic/restic/internal/backend/s3"
    "github.com/restic/restic/internal/backend/swift"
    "github.com/restic/restic/internal/options"
)

// Never persist these values in vaulticSettings. The application passes them
// directly to a private restic backend factory for the current session.
// Crucially, no process-wide os.Setenv is used (which would be a race and leak).
type vaulticCloudAuth struct {
    Scheme       string
    AccessID     string
    Secret       string
    AccountName  string
    Region       string
    AzureSAS     bool
    ProjectID    string // Google Cloud project (not a secret)

    // OpenStack Swift through Keystone v3. AccessID holds the user name, or the
    // application credential ID when AppCredential is set; Secret holds the
    // password or the application credential secret.
    AuthURL, UserDomain, Project, ProjectDomain string
    AppCredential bool
}

// hasValues reports whether the entry changes anything for its backend.
func (a vaulticCloudAuth) hasValues() bool {
    return a.Scheme != "" && (a.hasSecret() || a.ProjectID != "" || a.Region != "" || a.AccountName != "" || a.AuthURL != "")
}

// vaulticSwiftAuthURL checks a Keystone v3 endpoint. Credentials travel to this
// address, so it must be HTTPS (plain HTTP only for loopback test servers).
func vaulticSwiftAuthURL(raw string) (string, error) {
    raw = strings.TrimSpace(raw)
    u, err := url.Parse(raw)
    bad := errors.New("Indiquez l'URL Keystone v3 (ex. https://auth.exemple.fr/v3), sans identifiants ni paramètres.")
    if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(raw) > 512 {
        return "", bad
    }
    switch u.Scheme {
    case "https":
    case "http":
        if ip := net.ParseIP(u.Hostname()); u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
            return "", errors.New("HTTP n'est autorisé que vers localhost ; utilisez HTTPS pour Keystone.")
        }
    default:
        return "", bad
    }
    if !strings.Contains(u.Path, "/v3") {
        return "", errors.New("Seul Keystone v3 est pris en charge : l'URL doit contenir /v3.")
    }
    return raw, nil
}

// hasSecret reports whether this entry holds anything that must be wiped.
func (a vaulticCloudAuth) hasSecret() bool { return a.Secret != "" || a.AccessID != "" }

func (a vaulticCloudAuth) validate() error {
    switch a.Scheme {
    case "s3", "b2":
        if (a.AccessID == "") != (a.Secret == "") {
            return errors.New("Renseignez à la fois l'identifiant et la clé secrète, ou laissez les deux champs vides pour utiliser l'authentification déjà configurée.")
        }
    case "azure":
        if a.Secret != "" && a.AccountName == "" {
            return errors.New("Le nom du compte Azure est nécessaire avec une clé ou un SAS.")
        }
    case "rest":
        if (a.AccessID == "") != (a.Secret == "") {
            return errors.New("Renseignez l'utilisateur et le mot de passe du serveur REST, ou laissez les deux champs vides.")
        }
    case "swift":
        if !a.hasValues() {
            return nil // native authentication (OS_* environment)
        }
        if _, err := vaulticSwiftAuthURL(a.AuthURL); err != nil {
            return err
        }
        if a.AccessID == "" || a.Secret == "" {
            if a.AppCredential {
                return errors.New("Renseignez l'ID et le secret de l'Application Credential.")
            }
            return errors.New("Renseignez l'utilisateur et le mot de passe OpenStack.")
        }
        if !a.AppCredential && (a.UserDomain == "" || a.Project == "" || a.ProjectDomain == "") {
            return errors.New("Keystone v3 : domaine de l'utilisateur, projet et domaine du projet sont obligatoires.")
        }
    case "gs":
        if a.Secret != "" || a.AccessID != "" {
            return errors.New("Google Cloud utilise les Application Default Credentials ; aucune clé n'est saisie ici.")
        }
    default:
        return errors.New("Ce type de stockage n'utilise pas le formulaire cloud.")
    }
    return nil
}

func (a vaulticCloudAuth) secretValues() []string {
    if a.Secret==""{return nil}
    return []string{a.Secret}
}

type vaulticCloudSessions struct {
    mu sync.RWMutex
    entries map[string]vaulticCloudAuth
}
func (s *vaulticCloudSessions) set(repo string, auth vaulticCloudAuth) error {
    if err:=auth.validate();err!=nil{return err}
    scheme,err:=vaulticBackendLocation(repo)
    if err!=nil{return err}
    if scheme!=auth.Scheme{return errors.New("Type de dépôt incompatible avec les identifiants cloud.")}
    s.mu.Lock()
    defer s.mu.Unlock()
    if s.entries==nil{s.entries=make(map[string]vaulticCloudAuth)}
    s.entries[vaulticResolvedRepository(repo)]=auth
    return nil
}
func (s *vaulticCloudSessions) get(repo string) vaulticCloudAuth {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.entries[vaulticResolvedRepository(repo)]
}
// clearAll drops every credential held for this process (session end).
func (s *vaulticCloudSessions) clearAll(){
    s.mu.Lock()
    defer s.mu.Unlock()
    for key:=range s.entries{delete(s.entries,key)}
}
func (s *vaulticCloudSessions) forget(repo string){
    s.mu.Lock()
    defer s.mu.Unlock()
    delete(s.entries,vaulticResolvedRepository(repo))
}

// Wrap just the selected provider's factory. Every other registered backend
// remains the original restic implementation. The wrapper does not expose
// credentials via URL parameters, debug strings or a child process.
type vaulticPrivateFactory struct {
    location.Factory
    auth vaulticCloudAuth
}
func (f vaulticPrivateFactory) apply(cfg interface{}) (interface{},error) {
    switch f.auth.Scheme {
    case "s3":
        p,ok:=cfg.(*s3.Config)
        if !ok{return nil,errors.New("Configuration S3 invalide")}
        copy:=*p
        if f.auth.AccessID!=""{
            copy.KeyID=f.auth.AccessID
            copy.Secret=options.NewSecretString(f.auth.Secret)
        }
        if f.auth.Region!=""{copy.Region=f.auth.Region}
        return &copy,nil
    case "b2":
        p,ok:=cfg.(*b2.Config)
        if !ok{return nil,errors.New("Configuration B2 invalide")}
        copy:=*p
        if f.auth.AccessID!=""{
            copy.AccountID=f.auth.AccessID
            copy.Key=options.NewSecretString(f.auth.Secret)
        }
        return &copy,nil
    case "rest":
        p,ok:=cfg.(*rest.Config)
        if !ok||p.URL==nil{return nil,errors.New("Configuration REST invalide")}
        copy:=*p
        if f.auth.AccessID!=""{
            u:=*p.URL
            u.User=url.UserPassword(f.auth.AccessID,f.auth.Secret)
            copy.URL=&u
        }
        return &copy,nil
    case "swift":
        p,ok:=cfg.(*swift.Config)
        if !ok{return nil,errors.New("Configuration Swift invalide")}
        copy:=*p
        if f.auth.AuthURL=="" {return &copy,nil} // native authentication
        // Session credentials replace everything the environment may have
        // filled in, so ambient OS_* variables can never be mixed in.
        copy.UserName,copy.UserID,copy.Domain,copy.DomainID,copy.APIKey = "","","","",""
        copy.Tenant,copy.TenantID,copy.TenantDomain,copy.TenantDomainID,copy.TrustID = "","","","",""
        copy.StorageURL,copy.AuthToken = "",options.SecretString{}
        copy.ApplicationCredentialID,copy.ApplicationCredentialName = "",""
        copy.ApplicationCredentialSecret = options.SecretString{}
        copy.AuthURL,copy.Region = f.auth.AuthURL,f.auth.Region
        if f.auth.AppCredential {
            copy.ApplicationCredentialID=f.auth.AccessID
            copy.ApplicationCredentialSecret=options.NewSecretString(f.auth.Secret)
        }else{
            copy.UserName,copy.APIKey = f.auth.AccessID,f.auth.Secret
            copy.Domain,copy.Tenant,copy.TenantDomain = f.auth.UserDomain,f.auth.Project,f.auth.ProjectDomain
        }
        return &copy,nil
    case "gs":
        p,ok:=cfg.(*gs.Config)
        if !ok{return nil,errors.New("Configuration Google Cloud invalide")}
        copy:=*p
        if f.auth.ProjectID!=""{copy.ProjectID=f.auth.ProjectID}
        return &copy,nil
    case "azure":
        p,ok:=cfg.(*azure.Config)
        if !ok{return nil,errors.New("Configuration Azure invalide")}
        copy:=*p
        if f.auth.AccountName!=""{copy.AccountName=f.auth.AccountName}
        if f.auth.Secret!=""{
            if f.auth.AzureSAS {
                copy.AccountSAS=options.NewSecretString(f.auth.Secret)
                copy.AccountKey=options.SecretString{}
            }else{
                copy.AccountKey=options.NewSecretString(f.auth.Secret)
                copy.AccountSAS=options.SecretString{}
            }
        }
        return &copy,nil
    }
    return nil,fmt.Errorf("Type cloud non pris en charge")
}
func (f vaulticPrivateFactory) Create(ctx context.Context,cfg interface{},rt http.RoundTripper,lim limiter.Limiter,onError func(string,...interface{}))(backend.Backend,error){
    secure,err:=f.apply(cfg)
    if err!=nil{return nil,err}
    return f.Factory.Create(ctx,secure,rt,lim,onError)
}
func (f vaulticPrivateFactory) Open(ctx context.Context,cfg interface{},rt http.RoundTripper,lim limiter.Limiter,onError func(string,...interface{}))(backend.Backend,error){
    secure,err:=f.apply(cfg)
    if err!=nil{return nil,err}
    return f.Factory.Open(ctx,secure,rt,lim,onError)
}

// A backend registry exists per restic operation. It can safely be used by
// workers while a different repository is selected in the GUI.
func vaulticBackendsForSession(auth vaulticCloudAuth) *location.Registry {
    original:=all.Backends()
    if auth.Scheme==""{return original}
    registry:=location.NewRegistry()
    for _,info:=range vaulticBackendCatalog {
        factory:=original.Lookup(info.Scheme)
        if factory==nil{continue}
        if info.Scheme==auth.Scheme {
            factory=vaulticPrivateFactory{Factory:factory,auth:auth}
        }
        registry.Register(factory)
    }
    return registry
}

func vaulticCloudSessionDescription(auth vaulticCloudAuth) string {
    if auth.hasSecret() {return "Identifiants cloud gardés uniquement en mémoire jusqu'à la fermeture de Vaultic."}
    return "Authentification provenant de la configuration système ou de l'environnement."
}

func vaulticRedactCloudSecrets(redact func(string)string,auth vaulticCloudAuth)func(string)string{
    return func(message string)string{
        message=redact(message)
        for _,secret:=range auth.secretValues(){
            if len(strings.TrimSpace(secret))>0{message=strings.ReplaceAll(message,secret,"[SECRET MASQUÉ]")}
        }
        return message
    }
}
