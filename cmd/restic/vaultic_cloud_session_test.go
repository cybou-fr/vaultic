package main

import (
    "strings"
    "os"
    "encoding/json"
    "testing"

    "github.com/restic/restic/internal/backend/all"
    "github.com/restic/restic/internal/backend/azure"
    "github.com/restic/restic/internal/backend/b2"
    "github.com/restic/restic/internal/backend/s3"
)

func TestVaulticCloudSessionNeverChangesEnvironment(t *testing.T){
    t.Setenv("AWS_ACCESS_KEY_ID","ambient-key")
    auth:=vaulticCloudAuth{Scheme:"s3",AccessID:"private-id",Secret:"private-secret",Region:"eu-west-3"}
    var store vaulticCloudSessions
    if err:=store.set("s3:s3.amazonaws.com/bucket/repo",auth);err!=nil{t.Fatal(err)}
    if got:=store.get("s3:s3.amazonaws.com/bucket/repo");got.Secret!="private-secret"{t.Fatal("session credentials not found")}
    if got:=store.get("s3:s3.amazonaws.com/other/repo");got.Secret!=""{t.Fatal("credentials leaked to another repo")}
    if !strings.Contains(vaulticRedactCloudSecrets(func(s string)string{return s},auth)("error private-secret"),"[SECRET MASQUÉ]"){
        t.Fatal("cloud key was not redacted")
    }
    if val:=os.Getenv("AWS_ACCESS_KEY_ID");val!="ambient-key"{t.Fatalf("process environment changed: %q",val)}
    store.forget("s3:s3.amazonaws.com/bucket/repo")
    if got:=store.get("s3:s3.amazonaws.com/bucket/repo");got.Secret!=""{t.Fatal("forget did not release credentials")}
}

func TestVaulticCloudAuthValidatePairs(t *testing.T){
    for _,auth:=range []vaulticCloudAuth{
        {Scheme:"s3",AccessID:"only"},
        {Scheme:"b2",Secret:"only"},
        {Scheme:"azure",Secret:"key"},
        {Scheme:"unknown"},
    }{
        if err:=auth.validate();err==nil{t.Errorf("invalid auth accepted: %s",auth.Scheme)}
    }
    for _,auth:=range []vaulticCloudAuth{{Scheme:"s3"},{Scheme:"b2"},{Scheme:"azure"},{Scheme:"s3",AccessID:"a",Secret:"b"}}{
        if err:=auth.validate();err!=nil{t.Errorf("%s auth denied: %v",auth.Scheme,err)}
    }
}

func TestVaulticPrivateFactoryInjectsOnlySelectedBackend(t *testing.T){
    s3Auth:=vaulticCloudAuth{Scheme:"s3",AccessID:"id",Secret:"secret",Region:"eu-west-3"}
    reg:=vaulticBackendsForSession(s3Auth)
    s3Factory,ok:=reg.Lookup("s3").(vaulticPrivateFactory)
    if !ok{t.Fatal("S3 wrapper missing")}
    base:=s3.NewConfig()
    copyObj,err:=s3Factory.apply(&base)
    if err!=nil{t.Fatal(err)}
    c:=copyObj.(*s3.Config)
    if c.KeyID!="id"||c.Secret.String()==""||c.Region!="eu-west-3"{t.Fatal("S3 credentials not applied")}
    if base.KeyID!="" || base.Region!=""{t.Fatal("original shared config modified")}
    if _,ok:=reg.Lookup("b2").(vaulticPrivateFactory);ok{t.Fatal("unrelated B2 backend was wrapped")}
    if _,ok:=all.Backends().Lookup("s3").(vaulticPrivateFactory);ok{t.Fatal("original restic backend globally modified")}
    b2Obj:=b2.NewConfig()
    b2Result,err:=(vaulticPrivateFactory{auth:vaulticCloudAuth{Scheme:"b2",AccessID:"account",Secret:"key"}}).apply(&b2Obj)
    if err!=nil || b2Result.(*b2.Config).AccountID!="account"{t.Fatalf("B2 injection: %v",err)}
    azObj:=azure.NewConfig()
    azResult,err:=(vaulticPrivateFactory{auth:vaulticCloudAuth{Scheme:"azure",AccountName:"sample",Secret:"sig",AzureSAS:true}}).apply(&azObj)
    if err!=nil || azResult.(*azure.Config).AccountName!="sample" || azResult.(*azure.Config).AccountSAS.String()==""{t.Fatalf("Azure injection: %v",err)}
    if azResult.(*azure.Config).AccountKey.String()!=""{t.Fatal("Azure key/SAS cross-contaminated")}
}

func TestVaulticCloudCredentialsNeverSerializedToSettings(t *testing.T){
    var sessions vaulticCloudSessions
    if err:=sessions.set("b2:samplebucket:vaultic",vaulticCloudAuth{Scheme:"b2",AccessID:"private-id",Secret:"DO_NOT_WRITE_THIS_SECRET"});err!=nil{t.Fatal(err)}
    encoded,err:=json.Marshal(vaulticSettings{Repository:"b2:samplebucket:vaultic",Depots:[]vaulticDepot{{Name:"B2",Location:"b2:samplebucket:vaultic"}}})
    if err!=nil{t.Fatal(err)}
    if strings.Contains(string(encoded),"DO_NOT_WRITE_THIS_SECRET"){t.Fatal("cloud secret stored in preferences")}
    if err:=sessions.set("s3:s3.amazonaws.com/bucket/vaultic",vaulticCloudAuth{Scheme:"b2",AccessID:"id",Secret:"key"});err==nil{
        t.Fatal("cross-backend credentials accepted")
    }
}
