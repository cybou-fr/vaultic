package main

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// Cloud setup is a guided, session-only credential editor. Opening a repository
// verifies its password and remote connection through restic's Go engine.
// Creation uses the same engine but is always a distinct, explicit action.
func vaulticShowCloudWizard(parent fyne.Window, scheme string, submit func(vaulticCloudSetup, bool) error) {
	providerName := ""
	switch scheme {
	case "s3":
		providerName = "S3 / MinIO"
	case "b2":
		providerName = "Backblaze B2"
	case "azure":
		providerName = "Azure Blob"
	default:
		dialog.ShowInformation(vaulticT("Assistant non disponible"), vaulticT("Les autres types restent accessibles via l'adresse restic et leurs identifiants système."), parent)
		return
	}

	name := widget.NewEntry()
	name.SetText(vaulticT("Sauvegarde ") + providerName)
	pass := widget.NewPasswordEntry()
	pass.SetPlaceHolder(vaulticT("Mot de passe de chiffrement du dépôt restic"))
	bucket := widget.NewEntry()
	bucket.SetPlaceHolder(vaulticT("Bucket ou conteneur déjà créé"))
	prefix := widget.NewEntry()
	prefix.SetText("vaultic")

	endpoint := widget.NewEntry()
	endpoint.SetPlaceHolder(vaulticT("s3.amazonaws.com ou minio.exemple.fr:9000"))
	region := widget.NewEntry()
	region.SetPlaceHolder(vaulticT("eu-west-3 (facultatif)"))
	access := widget.NewEntry()
	access.SetPlaceHolder(vaulticT("Identifiant / Access Key"))
	secret := widget.NewPasswordEntry()
	secret.SetPlaceHolder(vaulticT("Clé secrète (jamais enregistrée)"))
	account := widget.NewEntry()
	account.SetPlaceHolder(vaulticT("Compte de stockage Azure"))
	azureAuth := widget.NewSelect([]string{vaulticT("Clé de compte"), vaulticT("Jeton SAS")}, nil)
	azureAuth.SetSelected(vaulticT("Clé de compte"))

	items := []*widget.FormItem{
		widget.NewFormItem(vaulticT("Nom dans Vaultic"), name),
		widget.NewFormItem(vaulticT("Mot de passe restic"), pass),
	}
	switch scheme {
	case "s3":
		items = append(items,
			widget.NewFormItem(vaulticT("Endpoint HTTPS"), endpoint),
			widget.NewFormItem(vaulticT("Bucket"), bucket),
			widget.NewFormItem(vaulticT("Chemin interne"), prefix),
			widget.NewFormItem(vaulticT("Région"), region),
			widget.NewFormItem(vaulticT("Access Key ID"), access),
			widget.NewFormItem(vaulticT("Secret Access Key"), secret),
		)
	case "b2":
		items = append(items,
			widget.NewFormItem(vaulticT("Bucket"), bucket),
			widget.NewFormItem(vaulticT("Chemin interne"), prefix),
			widget.NewFormItem(vaulticT("Account ID / Key ID"), access),
			widget.NewFormItem(vaulticT("Application Key"), secret),
		)
	case "azure":
		items = append(items,
			widget.NewFormItem(vaulticT("Compte Azure"), account),
			widget.NewFormItem(vaulticT("Conteneur"), bucket),
			widget.NewFormItem(vaulticT("Chemin interne"), prefix),
			widget.NewFormItem(vaulticT("Authentification"), azureAuth),
			widget.NewFormItem(vaulticT("Clé ou SAS"), secret),
		)
	}

	note := vaulticWrappedLabel(vaulticT("Les clés cloud restent en mémoire jusqu'à la fermeture de Vaultic et ne sont pas enregistrées dans les préférences. Laissez les champs de clés vides pour utiliser les identifiants déjà configurés dans le système."))
	note2 := vaulticWrappedLabel(vaulticT("Ouvrir : vérifie les identifiants et le dépôt existant. Créer : initialise un NOUVEAU dépôt restic dans un bucket ou conteneur déjà disponible."))
	form := widget.NewForm(items...)
	openButton := widget.NewButton(vaulticT("Tester et ouvrir"), nil)
	openButton.Importance = widget.HighImportance
	createButton := widget.NewButton(vaulticT("Créer un dépôt"), nil)
	controls := container.NewGridWithColumns(2, openButton, createButton)
	body := container.NewBorder(
		container.NewVBox(vaulticHeading(vaulticT("Connecter ")+providerName, 22), vaulticCaption(vaulticT("1. Dépôt  →  2. Identifiants  →  3. Vérification"))),
		container.NewVBox(note, note2, controls),
		nil, nil,
		container.NewVScroll(form),
	)
	dlg := dialog.NewCustom(vaulticT("Assistant de connexion cloud"), vaulticT("Fermer"), body, parent)
	dlg.Resize(fyne.NewSize(690, 640))
	perform := func(create bool) {
		data := vaulticCloudFormData{
			Scheme:   scheme,
			Name:     name.Text,
			Password: pass.Text,
			Endpoint: endpoint.Text,
			Bucket:   bucket.Text,
			Prefix:   prefix.Text,
			Region:   region.Text,
			Account:  account.Text,
			AccessID: access.Text,
			Secret:   secret.Text,
			AzureSAS: azureAuth.Selected == vaulticT("Jeton SAS"),
		}
		setup, err := vaulticBuildCloudSetup(data)
		if err != nil {
			dialog.ShowError(err, parent)
			return
		}
		if err := submit(setup, create); err != nil {
			dialog.ShowError(err, parent)
			return
		}
		pass.SetText("")
		secret.SetText("")
		dlg.Hide()
	}
	openButton.OnTapped = func() { perform(false) }
	createButton.OnTapped = func() { perform(true) }
	dlg.Show()
}

// Re-authorize an already registered repository after relaunching Vaultic.
// This form edits session credentials, never the saved repository location.
func vaulticShowCloudSessionWizard(parent fyne.Window, scheme string, apply func(vaulticCloudAuth) error) {
	id := widget.NewEntry()
	id.SetPlaceHolder(vaulticT("Identifiant / Access Key / Account ID"))
	account := widget.NewEntry()
	account.SetPlaceHolder(vaulticT("Nom du compte Azure"))
	secret := widget.NewPasswordEntry()
	secret.SetPlaceHolder(vaulticT("Clé d'accès cloud"))
	region := widget.NewEntry()
	region.SetPlaceHolder(vaulticT("Région S3 (facultatif)"))
	mode := widget.NewSelect([]string{vaulticT("Clé de compte"), vaulticT("Jeton SAS")}, nil)
	mode.SetSelected(vaulticT("Clé de compte"))
	project := widget.NewEntry()
	project.SetPlaceHolder(vaulticT("ID du projet (facultatif)"))
	keystone := widget.NewEntry()
	userDomain := widget.NewEntry()
	projectDomain := widget.NewEntry()
	swiftMode := widget.NewSelect([]string{vaulticSwiftPasswordChoice, vaulticSwiftAppCredChoice}, nil)
	fields := []*widget.FormItem{}
	switch scheme {
	case "s3":
		fields = append(fields,
			widget.NewFormItem(vaulticT("Access Key ID"), id),
			widget.NewFormItem(vaulticT("Secret Access Key"), secret),
			widget.NewFormItem(vaulticT("Région"), region),
		)
	case "b2":
		fields = append(fields,
			widget.NewFormItem(vaulticT("Account ID / Key ID"), id),
			widget.NewFormItem(vaulticT("Application Key"), secret),
		)
	case "azure":
		fields = append(fields,
			widget.NewFormItem(vaulticT("Compte Azure"), account),
			widget.NewFormItem(vaulticT("Authentification"), mode),
			widget.NewFormItem(vaulticT("Clé ou SAS"), secret),
		)
	case "rest":
		fields = append(fields,
			widget.NewFormItem(vaulticT("Utilisateur"), id),
			widget.NewFormItem(vaulticT("Mot de passe du serveur"), secret),
		)
	case "gs":
		fields = append(fields, widget.NewFormItem(vaulticT("ID du projet Google Cloud"), project))
	case "swift":
		keystone.SetPlaceHolder(vaulticT("https://auth.exemple.fr/v3"))
		swiftMode.SetSelected(vaulticSwiftPasswordChoice)
		userDomain.SetText("Default")
		projectDomain.SetText("Default")
		fields = append(fields,
			widget.NewFormItem(vaulticT("URL Keystone v3"), keystone),
			widget.NewFormItem(vaulticT("Authentification"), swiftMode),
			widget.NewFormItem(vaulticT("Utilisateur / ID"), id),
			widget.NewFormItem(vaulticT("Mot de passe / secret"), secret),
			widget.NewFormItem(vaulticT("Domaine utilisateur"), userDomain),
			widget.NewFormItem(vaulticT("Projet"), project),
			widget.NewFormItem(vaulticT("Domaine du projet"), projectDomain),
			widget.NewFormItem(vaulticT("Région"), region),
		)
	default:
		dialog.ShowInformation(vaulticT("Backend"), vaulticT("Les identifiants de ce backend sont gérés par sa configuration système (SSH, rclone, variables OpenStack…)."), parent)
		return
	}
	confirm := widget.NewButton(vaulticT("Utiliser pour cette session"), nil)
	confirm.Importance = widget.HighImportance
	body := container.NewVBox(
		vaulticHeading(vaulticT("Accès cloud temporaire"), 21),
		vaulticWrappedLabel(vaulticT("Ces identifiants seront utilisés uniquement en mémoire. Ils ne sont pas sauvegardés. Laissez les champs vides pour utiliser l'authentification déjà configurée dans le système.")),
		widget.NewForm(fields...),
		confirm,
	)
	dlg := dialog.NewCustom(vaulticT("Accès cloud"), vaulticT("Fermer"), body, parent)
	height := float32(460)
	if scheme == "swift" {
		height = 660
	}
	dlg.Resize(fyne.NewSize(590, height))
	confirm.OnTapped = func() {
		auth := vaulticCloudAuth{
			Scheme:      scheme,
			AccessID:    id.Text,
			Secret:      secret.Text,
			Region:      strings.TrimSpace(region.Text),
			AccountName: strings.TrimSpace(account.Text),
			AzureSAS:    mode.Selected == vaulticT("Jeton SAS"),
			ProjectID:   strings.TrimSpace(project.Text),
		}
		if scheme == "swift" {
			auth.ProjectID = ""
			auth.AuthURL = strings.TrimSpace(keystone.Text)
			auth.UserDomain = strings.TrimSpace(userDomain.Text)
			auth.ProjectDomain = strings.TrimSpace(projectDomain.Text)
			auth.Project = strings.TrimSpace(project.Text)
			auth.AppCredential = swiftMode.Selected == vaulticSwiftAppCredChoice
			auth.AccountName = ""
		}
		if err := auth.validate(); err != nil {
			dialog.ShowError(err, parent)
			return
		}
		if err := apply(auth); err != nil {
			dialog.ShowError(err, parent)
			return
		}
		secret.SetText("")
		dlg.Hide()
	}
	dlg.Show()
}
