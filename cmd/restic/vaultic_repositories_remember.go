package main

import (
	"errors"

	"fyne.io/fyne/v2/dialog"
)

// Remembering a repository password in the operating system's credential
// store. The choice is per repository and opt-in; Vaultic's own files only
// record the flag, never the password.

// syncRemember shows whether the active repository's password is remembered,
// without triggering the check box's own handler.
func (c *vaulticRepositoriesController) syncRemember() {
	depot, saved := c.savedDepot(c.active)
	c.syncing = true
	c.Remember.SetChecked(saved && depot.RememberPassword)
	c.syncing = false
}

// LoadRemembered fills the password field from the system credential store
// when the user chose to remember the active repository's password. A missing
// credential clears the choice and says so, so nothing stays ticked that does
// not work.
func (c *vaulticRepositoriesController) LoadRemembered() {
	depot, saved := c.savedDepot(c.active)
	if !saved || !depot.RememberPassword || c.Password.Text != "" {
		return
	}
	if !c.secrets.Available() {
		c.hooks.Log("Mot de passe mémorisé indisponible : " + c.secrets.Name() + " n'est pas accessible.")
		return
	}
	secret, err := c.secrets.Get(vaulticSecretKey(depot.Location))
	if err != nil {
		if errors.Is(err, errSecretNotFound) {
			c.setRemember(depot.Location, false)
			c.hooks.Log("Le mot de passe mémorisé pour « " + depot.Name + " » n'existe plus dans " + c.secrets.Name() + " : saisissez-le de nouveau.")
		} else {
			c.hooks.Log("Lecture du mot de passe mémorisé impossible : " + err.Error())
		}
		return
	}
	c.Password.SetText(secret)
	c.hooks.Log("Mot de passe de « " + depot.Name + " » chargé depuis " + c.secrets.Name() + ".")
}

// setRemember stores the flag of a repository in the settings (not the secret).
func (c *vaulticRepositoriesController) setRemember(location string, on bool) bool {
	next := vaulticSetRememberPassword(c.settings.Depots, location, on)
	if err := c.hooks.Persist(next, c.active); err != nil {
		c.hooks.Log("Erreur d'enregistrement des préférences : " + err.Error())
		return false
	}
	c.settings.Depots = next
	c.syncRemember()
	return true
}

func (c *vaulticRepositoriesController) onRememberChanged(on bool) {
	if c.syncing {
		return
	}
	w := c.hooks.Window
	depot, saved := c.savedDepot(c.active)
	if !saved {
		c.syncRemember()
		return
	}
	key := vaulticSecretKey(depot.Location)
	if !on {
		if err := c.secrets.Delete(key); err != nil {
			dialog.ShowError(err, w)
		}
		c.setRemember(depot.Location, false)
		c.hooks.Log("Mot de passe de « " + depot.Name + " » effacé de " + c.secrets.Name() + ".")
		return
	}
	if c.Password.Text == "" {
		dialog.ShowInformation("Mot de passe requis", "Saisissez d'abord le mot de passe du dépôt, puis cochez cette case.", w)
		c.syncRemember()
		return
	}
	if err := c.secrets.Set(key, c.Password.Text); err != nil {
		dialog.ShowError(err, w)
		c.syncRemember()
		return
	}
	if !c.setRemember(depot.Location, true) {
		_ = c.secrets.Delete(key)
		c.syncRemember()
		return
	}
	c.hooks.Log("Mot de passe de « " + depot.Name + " » mémorisé dans " + c.secrets.Name() + " (jamais dans les fichiers Vaultic).")
}

// moveRemembered keeps a remembered password with its repository when the
// repository's address is edited.
func (c *vaulticRepositoriesController) moveRemembered(oldLocation, newLocation string) {
	depot, ok := vaulticFindSavedDepot(c.settings.Depots, oldLocation)
	if !ok || !depot.RememberPassword || !c.secrets.Available() {
		return
	}
	secret, err := c.secrets.Get(vaulticSecretKey(oldLocation))
	if err == nil {
		err = c.secrets.Set(vaulticSecretKey(newLocation), secret)
	}
	if err != nil {
		c.hooks.Log("Mot de passe mémorisé non transféré vers la nouvelle adresse : " + err.Error())
		return
	}
	_ = c.secrets.Delete(vaulticSecretKey(oldLocation))
}
