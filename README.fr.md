# Vaultic — Sauvegardes chiffrées, moteur restic intégré

**Vaultic** est une application de sauvegarde native, développée en Go et Fyne pour Windows, Linux et macOS. Le code du moteur officiel **restic v0.19.1** est conservé directement dans ce dépôt, aux côtés du code de l'interface.

**Pas de programme restic externe. Pas de téléchargement du code source restic pendant la compilation.**

## Code source dans ce dépôt

```text
go.mod / go.sum                   Module Go complet (chemin d'import restic conservé)
cmd/restic/main.go               Point d'entrée du binaire Vaultic et commandes restic
cmd/restic/vaultic_*.go          Interface Vaultic, restauration ciblée, tests
cmd/restic/cmd_*.go              Fonctionnalités d'origine restic
internal/                        Moteur original restic : chiffrement, dépôts, archivage
scripts/build.py                Compilation locale du code présent dans le dépôt
scripts/test-local.py           Tests locaux lancés volontairement
LICENSE                         Licence Apache-2.0 du code Vaultic
LICENSE.restic                  Licence BSD-2-Clause du code restic
NOTICE / THIRD_PARTY_NOTICES.md Mentions légales
```

Le code du moteur reprend la version officielle **restic v0.19.1** (commit `6aa3a516ce654808a1f28f9fa21e9b7c8e6e90bf`). Ses chemins d'import `github.com/restic/restic` sont conservés pour ne pas réécrire artificiellement ses packages internes. **Cela ne signifie pas que le moteur est téléchargé : tous les packages internes sont déjà présents dans ce dépôt.**

L'interface est en **français**. Une traduction anglaise est prévue.

## Compilation locale

Prérequis : Go **1.25.10+**, Python 3 et les bibliothèques natives/C requises par [Fyne](https://docs.fyne.io/started/).

```bash
python3 scripts/build.py
```

Sur Windows :

```powershell
py scripts/build.py
```

Il est également possible de compiler directement :

```bash
go mod tidy
go build -o vaultic ./cmd/restic
```

Les commandes `go mod tidy` / `go build` peuvent télécharger les **dépendances Go tierces** (dont Fyne), comme dans tout projet Go. **Elles ne clonent pas restic**, dont les sources sont intégrées. Un lancement sans argument ouvre le GUI ; des arguments utilisent les commandes historiques restic (`vaultic version`, par exemple).

## Interface Vaultic

L'interface de bureau Fyne utilise une palette nuit bleu profond avec un accent turquoise, des surfaces arrondies et une navigation latérale. **Chaque écran a une fonction claire** :

- **Vue d'ensemble** : informations du dépôt, nombre de sources, instantanés consultés et activité de la session.
- **Sauvegarde** : choix des fichiers et dossiers à protéger, lancement manuel.
- **Instantanés** : historique, explorateur et restauration complète ou sélective.
- **Intégrité** : vérification du dépôt sans suppression.
- **Activité** : journal de la session et messages restic.
- **Paramètres** : chemin du dépôt, mot de passe en mémoire et initialisation.

Les données de tableau de bord restent volontairement neutres lorsqu'aucun instantané n'a été chargé : **Vaultic n'affiche jamais « protégé » sur la base d'informations inventées**. Le design est appliqué directement dans `cmd/restic/vaultic_ui_style.go`, `vaultic_ui_brand.go` et `vaultic_gui.go` : aucun framework web ni programme externe n'est nécessaire.

## Fonctionnalités en développement

- Dépôts chiffrés compatibles avec le format restic, local et distant.
- Sauvegarde et restauration depuis le moteur Go intégré.
- Historique et navigation dans les fichiers des instantanés.
- Restauration sélective d'un fichier ou d'un dossier, destination vide, sans écrasement.
- Vérification de dépôt, progression et interruption d'opérations.
- Préférences non secrètes, mode portable sur disque externe.

**État : version de développement**, non homologuée pour l'usage de sauvegardes irremplaçables. Les exécutables doivent être testés manuellement sur chaque OS avant publication.

## Tests locaux

```bash
python3 scripts/test-local.py
```

Windows : `py scripts/test-local.py`. Les tests Vaultic utilisent des dépôts temporaires. Les fichiers Go de test d'origine de restic sont conservés, mais **ses archives de test binaires et certains autres fichiers de test upstream ne sont pas inclus** ; la suite de tests complète de restic n'est donc pas garantie sans ces fixtures.

## Règle absolue : pas de CI

Aucune intégration continue, GitHub Actions, exécution planifiée ou déploiement automatique. Aucune configuration `.github/workflows`. La compilation et les tests sont uniquement déclenchés **manuellement, localement**.

## Licence

Code original Vaultic : [Apache-2.0](LICENSE). Code restic intégré : [BSD-2-Clause](LICENSE.restic). GUI Fyne : BSD-3-Clause. Voir [NOTICE](NOTICE) et [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). Vaultic est indépendant de restic et de Fyne.

English: [README.en.md](README.en.md).
