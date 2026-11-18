# Vaultic — Gestionnaire de sauvegardes chiffrées

**Vaultic** est une application de bureau multiplateforme, développée en Go et Fyne, qui pilote le moteur officiel **restic**.

L'interface de cette version est en **français**. Une localisation en anglais est prévue.

Vaultic utilise le format de dépôt standard de restic : vos sauvegardes restent compatibles avec la ligne de commande restic et les autres outils compatibles. Il n'existe pas de format propriétaire Vaultic.

## Version actuelle

**MVP 0.1 : code source expérimental**, sans installateur signé ni validation complète sur toutes les plateformes.

- Initialiser un dépôt chiffré local ou distant pris en charge par restic.
- Choisir les dossiers et fichiers à sauvegarder.
- Afficher la progression et interrompre une opération.
- Consulter les instantanés et restaurer un instantané dans un dossier vide.
- Vérifier la structure du dépôt avec `restic check`.
- Conserver les paramètres non secrets en mode normal ou portable.

Le mot de passe n'est pas enregistré par Vaultic : il reste en mémoire pendant la session et est transmis au processus restic. Conservez-le en lieu sûr, car sa perte rend la restauration impossible.

## Compilation

Prérequis : Go 1.23+, compilateur C, dépendances Fyne et exécutable restic.

```bash
go mod tidy
go run ./cmd/vaultic
```

Scripts de compilation : `scripts/build.ps1` (Windows) ou `scripts/build.sh` (Linux/macOS).

Pour les détails de sécurité, les tests et la configuration portable, consultez le [README en anglais](README.md).

## Licence

Vaultic est distribué sous **Apache License 2.0**. Voir [LICENSE](LICENSE), [NOTICE](NOTICE) et [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

Vaultic est un projet indépendant, sans affiliation officielle avec restic ou Fyne.
