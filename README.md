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
- **Stockage** : analyse manuelle des données référencées par les instantanés.
- **Intégrité** : vérification du dépôt sans suppression.
- **Activité** : journal de la session et messages restic.
- **Paramètres** : chemin du dépôt, mot de passe en mémoire et initialisation.

Les données de tableau de bord restent volontairement neutres lorsqu'aucun instantané n'a été chargé : **Vaultic n'affiche jamais « protégé » sur la base d'informations inventées**. Le design est appliqué directement dans `cmd/restic/vaultic_ui_style.go`, `vaultic_ui_brand.go` et `vaultic_gui.go` : aucun framework web ni programme externe n'est nécessaire.

## Fonctions disponibles pour les profils et le stockage

### Exclusions par profil

Dans **Sauvegarde → Exclusions…**, définissez les motifs d'exclusion restic pour le profil sélectionné, un par ligne (par exemple `**/node_modules`, `**/.cache`, `*.tmp`). Les lignes vides et les commentaires commençant par `#` sont ignorés. Les motifs sont vérifiés avant d'être enregistrés et transmis **directement à la fonction Go de sauvegarde restic**. Aucun shell n'est lancé.

Chaque profil possède ses propres exclusions persistées dans les préférences locales. Il ne s'agit pas d'un aperçu de fichiers réellement exclus : le moteur applique les motifs lors du backup.

### Stockage

L'écran **Stockage** déclenche manuellement l'opération native `stats` du moteur restic en mode `raw-data`. Il indique le nombre d'instantanés analysés, le nombre de blobs référencés et leur volume. **Ce volume n'est pas l'occupation complète du disque** : il exclut notamment les métadonnées et les données non référencées. Le calcul peut être interrompu et n'effectue pas de suppression.

## Gestion des dépôts et profils (sans suppression des sauvegardes)

Dans **Paramètres → Dépôt actif**, les boutons **Renommer…** et **Retirer de Vaultic…** ne modifient que la liste locale de dépôts. Renommer modifie uniquement le nom affiché ; retirer oublie le dépôt dans Vaultic, **sans supprimer le répertoire, les données, les instantanés ou les clés du dépôt restic**. Après le retrait du dépôt actif, Vaultic choisit le dépôt suivant s'il existe et demande à nouveau son mot de passe.

Dans **Sauvegarde**, les boutons **Dupliquer…** et **Supprimer le profil…** gèrent les profils locaux. Une copie conserve indépendamment les sources et exclusions de l'original. Supprimer un profil ne supprime aucun instantané restic ; le dernier profil ne peut pas être supprimé.

### Aperçu indicatif des exclusions

Le bouton **Aperçu des exclusions…** parcourt les sources locales en lecture seule pour trouver des chemins correspondant aux motifs restic du profil. Le résultat est limité à **25 000 entrées parcourues** et **80 exemples affichés** ; le nombre de correspondances n'est pas le nombre exact de fichiers exclus, car un répertoire exclu peut contenir de nombreux fichiers. Les règles de ré-inclusion `!`, les autorisations et les changements du système de fichiers peuvent modifier le résultat du backup réel. Cet aperçu ne crée pas de snapshot et ne remplace pas une vérification de restauration.

## Analyse et comparaison des instantanés

Dans **Instantanés**, sélectionnez un instantané puis :

- **Taille et fichiers…** : calcule le nombre de fichiers ordinaires, de dossiers, d'autres entrées et la taille logique totale des fichiers. Les tailles sont calculées en lisant les arborescences du dépôt, et non déduites du dernier journal de sauvegarde. Les liens physiques comptent pour chacun de leurs noms, et il ne s'agit pas de l'occupation physique du dépôt.
- **Comparer…** : choisissez un instantané de référence (avant) et comparez-le à l'instantané sélectionné (après). Vaultic utilise directement le moteur `diff` de restic, sans processus externe. Le résultat indique les fichiers ajoutés, supprimés et modifiés, les dossiers et les volumes de blobs concernés.

L'analyse des fichiers est **à la demande** ; les valeurs sont ensuite affichées dans la liste des instantanés pour la session courante et sont remises à zéro lors du changement de dépôt. La comparaison peut être annulée. Elle conserve en mémoire uniquement les 180 premières modifications, mais calcule les statistiques de l'ensemble du diff. Les différences de métadonnées seules ne sont pas affichées avec les options actuelles.

### Comparaison visuelle Avant / Après

Le bouton **Comparer…** ouvre maintenant une fenêtre dédiée avec **deux colonnes synchronisées** (instantané avant / après). Les chemins ajoutés n'apparaissent que dans la colonne après ; les chemins supprimés ne figurent que dans la colonne avant ; les modifications apparaissent dans les deux colonnes. Les filtres **Toutes / Ajoutés / Modifiés / Supprimés** et la recherche par nom ou chemin rendent la liste plus pratique à explorer.

Les nombres par catégorie sont calculés sur **l'ensemble** du diff restic, mais l'interface ne garde que **180 chemins par catégorie** (au plus 540 au total), pour limiter l'usage de mémoire sur les très grands dépôts. La recherche ne s'applique qu'aux chemins retenus dans cet aperçu ; les totaux complets restent affichés. Les éléments inchangés ne sont pas listés et les changements de métadonnées seuls ne sont pas activés. La comparaison est en lecture seule et ne modifie aucun instantané.

## Types de stockage pris en charge

Le sélecteur **Paramètres → Ajouter un dépôt → Type** présente les **neuf backends enregistrés** dans le moteur restic intégré : **local**, **SFTP**, **REST**, **S3**, **Backblaze B2**, **Azure Blob**, **Google Cloud Storage**, **OpenStack Swift** et **rclone**. Chaque sélection affiche un exemple d'adresse et les prérequis de connexion. Les adresses sont validées par le parseur restic réel.

- `local:` : dossiers, SSD, USB et NAS montés.
- `sftp:` : serveur SSH (client SSH externe requis).
- `rest:` : serveur REST restic.
- `s3:` : S3 et services compatibles, y compris MinIO selon endpoint.
- `b2:`, `azure:`, `gs:`, `swift:` : stockage objet chez le fournisseur indiqué.
- `rclone:` : remotes rclone (programme et configuration externes requis).

**Important :** la prise en charge d'un backend par le moteur ne signifie pas qu'une connexion ou une authentification cloud est automatiquement configurée. Les identifiants et variables d'environnement nécessaires doivent être fournis par le système ou le fournisseur ; Vaultic ne stocke pas de clés cloud dans ses préférences. Le mot de passe du dépôt restic reste distinct.

### Chronologie et restauration ciblée

La page **Instantanés** affiche maintenant une chronologie avec un marqueur de nouveau jour et des détails de taille quand ils ont été calculés. Depuis la comparaison **Avant / Après**, sélectionnez une ligne et choisissez **Ouvrir pour restaurer…** : Vaultic ouvre l'instantané approprié (avant pour les fichiers supprimés, après pour les ajouts ou modifications), dans son explorateur, avec le chemin déjà sélectionné lorsque le type de fichier est restaurable. Il faut encore **choisir un dossier de destination vide et confirmer** la restauration ; aucun fichier n'est remplacé automatiquement.

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
