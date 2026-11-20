package main

import (
    "fyne.io/fyne/v2"
    "fyne.io/fyne/v2/container"
    "fyne.io/fyne/v2/dialog"
    "fyne.io/fyne/v2/widget"
)

// Cloud setup is a guided, session-only credential editor. Opening a repository
// verifies its password and remote connection through restic's Go engine.
// Creation uses the same engine but is always a distinct, explicit action.
func vaulticShowCloudWizard(parent fyne.Window, scheme string, submit func(vaulticCloudSetup,bool)error){
    providerName:=""
    switch scheme{
    case "s3":providerName="S3 / MinIO"
    case "b2":providerName="Backblaze B2"
    case "azure":providerName="Azure Blob"
    default:
        dialog.ShowInformation("Assistant non disponible","Les autres types restent accessibles via l'adresse restic et leurs identifiants système.",parent)
        return
    }

    name:=widget.NewEntry()
    name.SetText("Sauvegarde "+providerName)
    pass:=widget.NewPasswordEntry()
    pass.SetPlaceHolder("Mot de passe de chiffrement du dépôt restic")
    bucket:=widget.NewEntry()
    bucket.SetPlaceHolder("Bucket ou conteneur déjà créé")
    prefix:=widget.NewEntry()
    prefix.SetText("vaultic")

    endpoint:=widget.NewEntry()
    endpoint.SetPlaceHolder("s3.amazonaws.com ou minio.exemple.fr:9000")
    region:=widget.NewEntry()
    region.SetPlaceHolder("eu-west-3 (facultatif)")
    access:=widget.NewEntry()
    access.SetPlaceHolder("Identifiant / Access Key")
    secret:=widget.NewPasswordEntry()
    secret.SetPlaceHolder("Clé secrète (jamais enregistrée)")
    account:=widget.NewEntry()
    account.SetPlaceHolder("Compte de stockage Azure")
    azureAuth:=widget.NewSelect([]string{"Clé de compte","Jeton SAS"},nil)
    azureAuth.SetSelected("Clé de compte")

    items:=[]*widget.FormItem{
        widget.NewFormItem("Nom dans Vaultic",name),
        widget.NewFormItem("Mot de passe restic",pass),
    }
    switch scheme {
    case "s3":
        items=append(items,
            widget.NewFormItem("Endpoint HTTPS",endpoint),
            widget.NewFormItem("Bucket",bucket),
            widget.NewFormItem("Chemin interne",prefix),
            widget.NewFormItem("Région",region),
            widget.NewFormItem("Access Key ID",access),
            widget.NewFormItem("Secret Access Key",secret),
        )
    case "b2":
        items=append(items,
            widget.NewFormItem("Bucket",bucket),
            widget.NewFormItem("Chemin interne",prefix),
            widget.NewFormItem("Account ID / Key ID",access),
            widget.NewFormItem("Application Key",secret),
        )
    case "azure":
        items=append(items,
            widget.NewFormItem("Compte Azure",account),
            widget.NewFormItem("Conteneur",bucket),
            widget.NewFormItem("Chemin interne",prefix),
            widget.NewFormItem("Authentification",azureAuth),
            widget.NewFormItem("Clé ou SAS",secret),
        )
    }

    note:=vaulticWrappedLabel("Les clés cloud restent en mémoire jusqu'à la fermeture de Vaultic et ne sont pas enregistrées dans les préférences. Laissez les champs de clés vides pour utiliser les identifiants déjà configurés dans le système.")
    note2:=vaulticWrappedLabel("Ouvrir : vérifie les identifiants et le dépôt existant. Créer : initialise un NOUVEAU dépôt restic dans un bucket ou conteneur déjà disponible.")
    form:=widget.NewForm(items...)
    openButton:=widget.NewButton("Tester et ouvrir",nil)
    openButton.Importance=widget.HighImportance
    createButton:=widget.NewButton("Créer un dépôt",nil)
    controls:=container.NewGridWithColumns(2,openButton,createButton)
    body:=container.NewBorder(
        container.NewVBox(vaulticHeading("Connecter "+providerName,22),vaulticCaption("1. Dépôt  →  2. Identifiants  →  3. Vérification")),
        container.NewVBox(note,note2,controls),
        nil,nil,
        container.NewVScroll(form),
    )
    dlg:=dialog.NewCustom("Assistant de connexion cloud","Fermer",body,parent)
    dlg.Resize(fyne.NewSize(690,640))
    perform:=func(create bool){
        data:=vaulticCloudFormData{
            Scheme:scheme,
            Name:name.Text,
            Password:pass.Text,
            Endpoint:endpoint.Text,
            Bucket:bucket.Text,
            Prefix:prefix.Text,
            Region:region.Text,
            Account:account.Text,
            AccessID:access.Text,
            Secret:secret.Text,
            AzureSAS:azureAuth.Selected=="Jeton SAS",
        }
        setup,err:=vaulticBuildCloudSetup(data)
        if err!=nil{dialog.ShowError(err,parent);return}
        if err:=submit(setup,create);err!=nil{dialog.ShowError(err,parent);return}
        pass.SetText("")
        secret.SetText("")
        dlg.Hide()
    }
    openButton.OnTapped=func(){perform(false)}
    createButton.OnTapped=func(){perform(true)}
    dlg.Show()
}

    
// Re-authorize an already registered repository after relaunching Vaultic.
// This form edits session credentials, never the saved repository location.
func vaulticShowCloudSessionWizard(parent fyne.Window, scheme string, apply func(vaulticCloudAuth)error) {
    id:=widget.NewEntry()
    id.SetPlaceHolder("Identifiant / Access Key / Account ID")
    account:=widget.NewEntry()
    account.SetPlaceHolder("Nom du compte Azure")
    secret:=widget.NewPasswordEntry()
    secret.SetPlaceHolder("Clé d'accès cloud")
    region:=widget.NewEntry()
    region.SetPlaceHolder("Région S3 (facultatif)")
    mode:=widget.NewSelect([]string{"Clé de compte","Jeton SAS"},nil)
    mode.SetSelected("Clé de compte")
    fields:=[]*widget.FormItem{}
    switch scheme {
    case "s3":
        fields=append(fields,
            widget.NewFormItem("Access Key ID",id),
            widget.NewFormItem("Secret Access Key",secret),
            widget.NewFormItem("Région",region),
        )
    case "b2":
        fields=append(fields,
            widget.NewFormItem("Account ID / Key ID",id),
            widget.NewFormItem("Application Key",secret),
        )
    case "azure":
        fields=append(fields,
            widget.NewFormItem("Compte Azure",account),
            widget.NewFormItem("Authentification",mode),
            widget.NewFormItem("Clé ou SAS",secret),
        )
    default:
        dialog.ShowInformation("Backend","Les identifiants de ce backend sont gérés par sa configuration système.",parent)
        return
    }
    confirm:=widget.NewButton("Utiliser pour cette session",nil)
    confirm.Importance=widget.HighImportance
    body:=container.NewVBox(
        vaulticHeading("Accès cloud temporaire",21),
        vaulticWrappedLabel("Ces identifiants seront utilisés uniquement en mémoire. Ils ne sont pas sauvegardés. Laissez les champs vides pour utiliser l'authentification déjà configurée dans le système."),
        widget.NewForm(fields...),
        confirm,
    )
    dlg:=dialog.NewCustom("Accès cloud","Fermer",body,parent)
    dlg.Resize(fyne.NewSize(590,460))
    confirm.OnTapped=func(){
        auth:=vaulticCloudAuth{
            Scheme:scheme,
            AccessID:id.Text,
            Secret:secret.Text,
            Region:region.Text,
            AccountName:account.Text,
            AzureSAS:mode.Selected=="Jeton SAS",
        }
        if err:=auth.validate();err!=nil{dialog.ShowError(err,parent);return}
        if err:=apply(auth);err!=nil{dialog.ShowError(err,parent);return}
        secret.SetText("")
        dlg.Hide()
    }
    dlg.Show()
}
