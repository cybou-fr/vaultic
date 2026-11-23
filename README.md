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
- **Dépôts** : choix du type de stockage, configuration guidée, mot de passe en mémoire, test de connexion et liste des dépôts.
- **Paramètres** : informations sur Vaultic.

Les données de tableau de bord restent volontairement neutres lorsqu'aucun instantané n'a été chargé : **Vaultic n'affiche jamais « protégé » sur la base d'informations inventées**. Le design est appliqué directement dans `cmd/restic/vaultic_ui_style.go`, `vaultic_ui_brand.go` et `vaultic_gui.go` : aucun framework web ni programme externe n'est nécessaire.

## Fonctions disponibles pour les profils et le stockage

### Exclusions par profil

Dans **Sauvegarde → Exclusions…**, définissez les motifs d'exclusion restic pour le profil sélectionné, un par ligne (par exemple `**/node_modules`, `**/.cache`, `*.tmp`). Les lignes vides et les commentaires commençant par `#` sont ignorés. Les motifs sont vérifiés avant d'être enregistrés et transmis **directement à la fonction Go de sauvegarde restic**. Aucun shell n'est lancé.

Chaque profil possède ses propres exclusions persistées dans les préférences locales. Il ne s'agit pas d'un aperçu de fichiers réellement exclus : le moteur applique les motifs lors du backup.

### Stockage

L'écran **Stockage** déclenche manuellement l'opération native `stats` du moteur restic en mode `raw-data`. Il indique le nombre d'instantanés analysés, le nombre de blobs référencés et leur volume. **Ce volume n'est pas l'occupation complète du disque** : il exclut notamment les métadonnées et les données non référencées. Le calcul peut être interrompu et n'effectue pas de suppression.

## Gestion des dépôts et profils (sans suppression des sauvegardes)

Dans **Dépôts**, les boutons **Modifier** et **Oublier** ne modifient que la liste locale de dépôts. Modifier change le nom affiché ou l'adresse enregistrée ; oublier retire le dépôt dans Vaultic, **sans supprimer le répertoire, les données, les instantanés ou les clés du dépôt restic**. Après le retrait du dépôt actif, Vaultic choisit le dépôt suivant s'il existe et demande à nouveau son mot de passe.

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

La page **Dépôts → Ajouter un dépôt** présente les **neuf backends enregistrés** dans le moteur restic intégré : **local**, **SFTP**, **REST**, **S3**, **Backblaze B2**, **Azure Blob**, **Google Cloud Storage**, **OpenStack Swift** et **rclone**. Chaque sélection affiche un exemple d'adresse et les prérequis de connexion. Les adresses sont validées par le parseur restic réel.

- `local:` : dossiers, SSD, USB et NAS montés.
- `sftp:` : serveur SSH (client SSH externe requis).
- `rest:` : serveur REST restic.
- `s3:` : S3 et services compatibles, y compris MinIO selon endpoint.
- `b2:`, `azure:`, `gs:`, `swift:` : stockage objet chez le fournisseur indiqué.
- `rclone:` : remotes rclone (programme et configuration externes requis).

**Important :** la prise en charge d'un backend par le moteur ne signifie pas qu'une connexion ou une authentification cloud est automatiquement configurée. Les identifiants et variables d'environnement nécessaires doivent être fournis par le système ou le fournisseur ; Vaultic ne stocke aucune clé cloud dans ses préférences (voir la page Dépôts ci-dessous pour la saisie en mémoire de session). Le mot de passe du dépôt restic reste distinct.

### Chronologie et restauration ciblée

La page **Instantanés** affiche maintenant une chronologie avec un marqueur de nouveau jour et des détails de taille quand ils ont été calculés. Depuis la comparaison **Avant / Après**, sélectionnez une ligne et choisissez **Ouvrir pour restaurer…** : Vaultic ouvre l'instantané approprié (avant pour les fichiers supprimés, après pour les ajouts ou modifications), dans son explorateur, avec le chemin déjà sélectionné lorsque le type de fichier est restaurable. Il faut encore **choisir un dossier de destination vide et confirmer** la restauration ; aucun fichier n'est remplacé automatiquement.

## Page Dépôts : connexion guidée aux neuf types de stockage

La page **Dépôts** réunit un seul objectif : choisir, configurer et vérifier un dépôt.

1. **Type de stockage** : neuf cartes (local, SFTP, REST, S3, Backblaze B2, Azure Blob, Google Cloud, OpenStack Swift, rclone) navigables au clavier (Tab, flèches, Entrée ou Espace).
2. **Emplacement** : un formulaire propre à chaque type, avec validation en ligne et un aperçu de l'adresse restic produite. Le port SSH est un champ dédié (forme `sftp://utilisateur@hôte:port//chemin`). Les adresses sont validées par le parseur restic réel et les valeurs dangereuses (hôte commençant par `-`, `..`, paramètres d'URL, identifiants dans l'URL) sont refusées.
3. **Nom et mot de passe restic** : le mot de passe de chiffrement du dépôt reste distinct et n'est jamais enregistré.

Boutons : **Tester la connexion**, **Ouvrir le dépôt existant**, **Créer un nouveau dépôt**. Les assistants pas à pas S3, B2 et Azure restent disponibles.

### États de connexion

*Non testé*, *Test en cours*, *Joignable*, *Authentification refusée*, *Dépôt indisponible* et *Connecté*. **Joignable** signifie seulement que le stockage répond et qu'un dépôt restic existe : sans mot de passe, Vaultic n'affiche jamais « Connecté ». Un mot de passe restic incorrect, des identifiants de stockage refusés, des droits insuffisants, une erreur réseau et l'absence de dépôt donnent des messages distincts. Chaque test a un délai maximal de 45 s et peut être annulé.

### Identifiants (session uniquement)

- REST (utilisateur et mot de passe du serveur), Google Cloud (ID de projet), S3, B2, Azure et **OpenStack Swift** (Keystone v3 : URL, région, domaines, projet, utilisateur et mot de passe, ou Application Credential) peuvent être saisis dans Vaultic.
- Ils restent **en mémoire**, isolés par dépôt, sans variable d'environnement globale, sans sous-processus, et ne figurent ni dans l'adresse, ni dans `vaultic.json`, ni dans les journaux. Ils sont effacés à la fermeture.
- Pour Swift, des identifiants saisis remplacent entièrement les variables `OS_*` du système ; champs vides = authentification système.
- Google Cloud utilise uniquement les Application Default Credentials ; SFTP utilise votre agent/clé SSH ; rclone utilise sa propre configuration.

### Liste des dépôts

Chaque dépôt enregistré affiche son nom, son fournisseur, son emplacement (sans identifiants), la dernière vérification et les actions **Se connecter**, **Modifier**, **Tester** et **Oublier**. **Oublier** retire uniquement l'entrée de Vaultic : aucune sauvegarde n'est supprimée, localement ou à distance.

### Limites connues

- Les connexions cloud (S3, B2, Azure, GCS, Swift, rclone, SFTP) n'ont pas encore été validées contre de vrais services ; REST et Swift sont testés avec des serveurs simulés, le stockage local avec de vrais dépôts temporaires.
- Le moteur restic **crée le conteneur Swift** s'il n'existe pas lors de l'ouverture, y compris pendant un test.
- Swift : Keystone v3 uniquement ; Application Credential par ID. L'URL Keystone doit être en HTTPS (HTTP seulement vers localhost), se terminer par `/v3` et ne contenir ni identifiants, ni paramètres, ni fragment ; elle est normalisée avant d'être gardée en mémoire.
- Google Cloud : pas de saisie de clé dans Vaultic (ADC uniquement).

## Automatisation des sauvegardes

Dans **Sauvegarde → Automatisation**, chaque profil possède sa propre planification : *manuelle uniquement*, *toutes les N heures*, *tous les jours à une heure choisie*, *chaque semaine* (jours au choix) ou *chaque mois* (jour au choix ; le 29, 30 ou 31 devient le dernier jour d'un mois plus court). Les heures sont celles du fuseau horaire local de l'ordinateur. Les options secondaires (jours, jour du mois, notifications) sont sous **Options avancées**.

La page affiche la fréquence, la **prochaine exécution**, la **dernière sauvegarde réussie**, l'activité en cours, l'historique récent et un bouton **Mettre en pause / Reprendre la planification**.

**Fonctionnement**

- Le planificateur ne travaille que **pendant que le processus Vaultic tourne**. Il n'y a ni service Windows, ni tâche du planificateur système, ni processus caché : en arrière-plan, Vaultic reste visible dans la zone de notification (menu : Ouvrir, Sauvegarder maintenant, Quitter).
- **Mot de passe mémorisé (facultatif)** : la case « Mémoriser le mot de passe sur cet ordinateur » (page Dépôts) place le mot de passe du dépôt dans le gestionnaire d'identifiants du système (Windows : Gestionnaire d'identifiants ; Linux : `secret-tool`/libsecret ; macOS : pas encore pris en charge). Vaultic n'enregistre que l'indicateur « mémorisé » dans `vaultic.json`, jamais le mot de passe. Décocher la case, ou « Oublier » le dépôt, efface le mot de passe du système. Sans cette case, rien ne change : le mot de passe reste saisi à chaque lancement.
- **Arrière-plan (Paramètres)** : « Fermer la fenêtre réduit Vaultic dans la zone de notification » et « Démarrer Vaultic avec ma session » (entrée utilisateur du démarrage Windows, fichier XDG sous Linux, LaunchAgent sous macOS ; lancement avec `--background`, fenêtre masquée).
- **Instance unique et journaux** : une seconde exécution de Vaultic (même fichier de paramètres) ne démarre pas ; elle demande à l'instance déjà ouverte de s'afficher. Les journaux (jamais de mots de passe) sont écrits dans `logs/vaultic.log` à côté des paramètres (5 fichiers de 1 Mio, rotation) ; Paramètres → Diagnostic ouvre ce dossier.
- **Conservation des instantanés (onglet « Conservation »)** : règles par profil (derniers N, un par jour, semaine, mois, année). **Rien n'est jamais supprimé automatiquement** : ni après une sauvegarde, ni par la planification. « Prévisualiser » liste exactement les instantanés qui seraient supprimés (et ceux conservés, avec la raison) sans rien modifier ; « Supprimer les instantanés listés… » demande une confirmation, revérifie le dépôt juste avant, et ne supprime que les identifiants prévisualisés, uniquement ceux du profil (même machine, mêmes sources). Le dernier instantané est toujours conservé. L'espace disque n'est rendu que par l'étape séparée « Libérer l'espace disque… » (prune). Chaque suppression est inscrite dans l'historique.
- **Langue (Paramètres)** : « Automatique », « Français » ou « English ». Le choix est enregistré dans `vaultic.json` (clé `language`) et s'applique au prochain démarrage. Les messages sont écrits en français, qui sert de clé ; le catalogue anglais est dans `cmd/restic/vaultic_i18n_en.go`. Un test vérifie que chaque message a sa traduction, que les formats (`%s`, `%d`) et les retours à la ligne correspondent, et qu'aucun texte français ne subsiste dans les écrans principaux en anglais. Pour ajouter une langue ou un message : encadrer le texte avec `vaulticT("…")`, puis compléter le catalogue (`VAULTIC_I18N_DUMP=fichier.txt go test ./cmd/restic -run EnglishCatalogCovers` liste les manques). Limite connue : quelques messages d'erreur du moteur restic et des messages assemblés par concaténation restent partiellement en français.
- **Version et mises à jour (Paramètres)** : la version et le commit compilés sont affichés dans « À propos ». « Rechercher une mise à jour » envoie **une seule requête anonyme** à l'API des versions de GitHub, uniquement quand vous appuyez sur le bouton ; rien n'est téléchargé ni installé. Procédure de publication : `docs/RELEASE.md` (`py scripts/package.py` produit les archives standard et portable et leurs sommes SHA-256).
- **Alerte de sauvegarde ancienne** : si la dernière sauvegarde réussie date de plus de deux fois l'intervalle prévu, un avertissement apparaît dans l'onglet Automatisation et une notification (au plus une par jour) est envoyée. Elle ne lance aucune sauvegarde.
- Chaque planification est liée au dépôt sélectionné au moment de son enregistrement. Changer le dépôt actif ne redirige jamais les sauvegardes planifiées. Le dépôt cible doit être actif et son **mot de passe doit être saisi** (il n'est jamais enregistré) ; sinon la sauvegarde reste en retard jusqu'à l'activation du bon dépôt et la saisie des identifiants.
- **Pas d'arriéré** : si plusieurs échéances ont été manquées (application fermée, ordinateur en veille, horloge modifiée), une seule sauvegarde de rattrapage est lancée. Reprendre après une pause ne rattrape rien.
- **Un seul traitement restic à la fois** : les sauvegardes manuelles, planifiées et les autres opérations partagent le même moteur. Si le moteur est occupé, la sauvegarde planifiée est reportée de 5 minutes, 3 fois au plus, puis **ignorée et consignée** jusqu'à l'échéance suivante. Il n'y a aucune nouvelle tentative silencieuse après un échec.
- Une sauvegarde n'est « réussie » que si restic confirme un identifiant d'instantané et n'a signalé aucun fichier illisible.
- Changements d'heure d'été/hiver, de fuseau horaire et d'horloge sont gérés : une échéance n'a lieu qu'une fois par jour calendaire, et une horloge remise en arrière relance simplement le décompte.
- Vaultic ne lance **jamais** `forget` ni `prune` et ne supprime aucun instantané automatiquement.

**Historique** : `vaultic-history.json` (à côté de `vaultic.json`, donc portable) conserve les 300 dernières exécutions : profil, dépôt (sans identifiants), déclencheur manuel/planifié, statut (réussie, échec, annulée, ignorée), durée, fichiers et octets, identifiant d'instantané et un court résumé d'erreur **assaini** (secrets masqués, une ligne). Aucun mot de passe, jeton ni message de diagnostic brut n'est enregistré. Les écritures sont atomiques.

**Notifications** du bureau : sauvegarde terminée, en échec, ignorée, en retard — chaque catégorie peut être désactivée. Un même événement n'est notifié qu'une fois et le nombre de notifications est limité.

**Limites** : une planification ne protège pas vos fichiers si Vaultic est fermé ou si le mot de passe n'est pas saisi ; chaque planification a un dépôt cible fixe, qui doit être actif au moment de l'exécution ; l'historique et la dernière sauvegarde réussie sont filtrés par dépôt ; les notifications dépendent de la prise en charge du système ; pas de politique de rétention dans cette version.

## Fonctionnalités en développement

- Dépôts chiffrés compatibles avec le format restic, local et distant.
- Sauvegarde et restauration depuis le moteur Go intégré.
- Historique et navigation dans les fichiers des instantanés.
- Restauration sélective d'un fichier ou d'un dossier, destination vide, sans écrasement.
- Vérification de dépôt, progression et interruption d'opérations.
- Sauvegardes planifiées pendant que Vaultic est ouvert, historique d'exécution et notifications.
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
