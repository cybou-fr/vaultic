package main

import (
 "errors"
 "fmt"
 "net"
 "net/url"
 "strings"
)

// vaulticGuidedStorageForm contains only non-secret repository location data.
// Credentials and the restic encryption password must be supplied separately.
type vaulticGuidedStorageForm struct {
 Scheme string
 Path string
 Host string
 Port string
 Username string
 Bucket string
 Prefix string
 Remote string
}

func vaulticGuidedStorageField(value string) (string, error) {
 value = strings.TrimSpace(value)
 if len(value) > 512 || strings.ContainsAny(value, "\x00\r\n?#") {
  return "", errors.New("Champ trop long ou contenant des caractères non autorisés.")
 }
 return value, nil
}

func vaulticGuidedStoragePrefix(raw string) (string, error) {
 p, err := vaulticGuidedStorageField(raw)
 if err != nil { return "", err }
 p = strings.Trim(p, "/")
 if strings.Contains(p, "\\") { return "", errors.New("Utilisez des barres obliques dans le chemin distant.") }
 if p != "" {
  for _, segment := range strings.Split(p, "/") {
   if segment == "" || segment == "." || segment == ".." { return "", errors.New("Chemin distant invalide.") }
  }
 }
 return p, nil
}

func vaulticBuildGuidedStorageAddress(form vaulticGuidedStorageForm) (string, error) {
 field := func(raw string) (string, error) { return vaulticGuidedStorageField(raw) }
 var address string
 switch form.Scheme {
 case "local":
  p, err := field(form.Path); if err != nil { return "", err }
  if p == "" { return "", errors.New("Choisissez un dossier de dépôt.") }
  address = "local:" + p
 case "rest":
  raw, err := field(form.Path); if err != nil { return "", err }
  u, err := url.Parse(raw)
  if err != nil || u == nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
   return "", errors.New("Indiquez une URL HTTPS sans identifiants ni paramètres.")
  }
  address = "rest:" + u.String()
 case "sftp":
  host, err := field(form.Host); if err != nil { return "", err }
  user, err := field(form.Username); if err != nil { return "", err }
  port, err := field(form.Port); if err != nil { return "", err }
  p, err := field(form.Path); if err != nil { return "", err }
  if host == "" || user == "" || !strings.HasPrefix(p, "/") || p == "/" || strings.ContainsAny(user, ":/@\\ ") || strings.ContainsAny(host, ":/@\\ ") || strings.ContainsAny(p, "\\") {
   return "", errors.New("Indiquez un utilisateur, un hôte et un chemin absolu SFTP valides.")
  }
  if port != "" {
   if len(port) > 5 { return "", errors.New("Port SSH invalide.") }
   for _, c := range port { if c < '0' || c > '9' { return "", errors.New("Port SSH invalide.") } }
   if port == "0" { return "", errors.New("Port SSH invalide.") }
   // The restic SFTP location parser accepts ssh port using a URL-style form;
   // prefer the default port in this first guided version.
   return "", errors.New("Pour un port SSH personnalisé, utilisez le mode adresse avancée.")
  }
  if net.ParseIP(host) == nil && strings.ContainsAny(host, " \t") { return "", errors.New("Hôte SFTP invalide.") }
  address = fmt.Sprintf("sftp:%s@%s:%s", user, host, p)
 case "gs", "swift":
  bucket, err := field(form.Bucket); if err != nil { return "", err }
  prefix, err := vaulticGuidedStoragePrefix(form.Prefix); if err != nil { return "", err }
  if bucket == "" || strings.ContainsAny(bucket, ":/@\\ \t") {
   return "", errors.New("Indiquez un bucket ou conteneur valide.")
  }
  address = form.Scheme + ":" + bucket + ":/" + prefix
 case "rclone":
  remote, err := field(form.Remote); if err != nil { return "", err }
  prefix, err := vaulticGuidedStoragePrefix(form.Prefix); if err != nil { return "", err }
  if remote == "" || strings.ContainsAny(remote, ":/@\\ \t") { return "", errors.New("Nom de remote rclone invalide.") }
  address = "rclone:" + remote + ":" + prefix
 default:
  return "", errors.New("Ce type de stockage n'est pas pris en charge par cet assistant.")
 }
 if err := vaulticValidateDepot(address); err != nil { return "", err }
 return address, nil
}
