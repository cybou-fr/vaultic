package main

import (
    "context"
    "errors"
    "fmt"
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
