# Vaultic — Restic-powered Backup Application

**Vaultic** est une application de sauvegarde native pour Windows, Linux et macOS, construite en **Go + Fyne** à partir des **sources officielles de restic**.

**Ce n'est pas une interface qui lance la commande restic.** Le moteur de restic et l'interface graphique sont compilés dans **un seul exécutable**. Aucun `restic.exe` séparé n'est requis à l'exécution.

- **Moteur :** restic v0.19.1, commit épinglé `6aa3a516ce654808a1f28f9fa21e9b7c8e6e90bf`.
- **Compatibilité :** format de dépôt restic officiel, inchangé.
- **Langue actuelle :** français. Traduction anglaise prévue.
- **Licence :** Apache-2.0 pour le code Vaultic, BSD-2-Clause pour les sources restic, BSD-3-Clause pour Fyne.
- **État :** intégration en développement, pas encore une version de production validée.

## Architecture

```text
Vaultic (exécutable unique)
├── Interface Fyne (overlay/vaultic_gui.go)
├── Adaptateur d'affichage des opérations restic
├── Appels Go natifs à runInit / runBackup / runSnapshots / runRestore / runCheck
└── Sources officielles de restic (v0.19.1)
    ├── archiver, repository, restorer
    ├── crypto, chunker, index
    └── backends locaux et distants
```

La compilation intègre les fichiers Vaultic dans `cmd/restic` du projet restic afin d'accéder aux opérations natives, sans réimplémenter le moteur ni utiliser de sous-processus. Lorsqu'on lance `vaultic` sans arguments, l'interface graphique s'ouvre. Les commandes CLI restic d'origine restent disponibles avec des arguments (par exemple `vaultic version`) pour le diagnostic.

## Compilation

Prérequis : **Go 1.25.10+**, Git, Python 3 et les dépendances graphiques/C de [Fyne](https://docs.fyne.io/started/).

```bash
python3 scripts/assemble.py
```

Sur Windows :

```powershell
py scripts/assemble.py
```

Le script récupère restic v0.19.1 depuis son dépôt officiel **pendant la compilation**, vérifie le commit source épinglé, copie le code d'intégration dans son arbre source, ajoute Fyne au module Go, puis génère `dist/vaultic` ou `dist/vaultic.exe`. Les téléchargements n'ont lieu que pendant la compilation.

Sur Linux, des dépendances de compilation OpenGL/X11 supplémentaires sont nécessaires. Les distributions finales devront être compilées et validées sur chaque système.

## Fonctionnalités initiales

- Initialisation d'un dépôt chiffré compatible restic.
- Sauvegarde de dossiers et fichiers, avec progression JSON native et annulation.
- Historique des instantanés.
- Restauration d'un instantané dans un dossier vide.
- Vérification de l'intégrité structurelle du dépôt.
- Préférences non sensibles (sans mot de passe), avec mode portable.

## Sécurité et limites

- Le mot de passe reste en mémoire, il n'est pas enregistré par Vaultic.
- La sauvegarde refuse un dépôt local situé sous une source sélectionnée.
- La restauration exige un dossier de destination existant et vide, pour éviter d'écraser des fichiers.
- Pas encore de planificateur, d'explorateur sélectif d'instantanés, de règles de rétention, de gestionnaire de secrets système ni de test de reprise complet.
- Les sauvegardes doivent être validées en test avec le CLI officiel avant usage pour des données importantes.

## Politique de développement — CI interdite

**Aucune intégration continue (CI) ni automatisation GitHub Actions n'est autorisée pour Vaultic.**

- Ne pas ajouter de fichiers de workflow sous `.github/workflows/`.
- Ne pas configurer de compilation, test, publication ou déploiement automatique sur push, pull request ou planification.
- Effectuer les compilations et les tests **manuellement**, en local, lorsque nécessaire.
- Ne pas réintroduire de service CI tiers sans décision explicite du propriétaire du projet.

Cette règle concerne l'automatisation CI, **pas** les tests manuels ni les scripts de compilation locale.

## Licences

Vaultic est un projet indépendant et n'est pas affilié officiellement à restic ni à Fyne. Le code propre à Vaultic est sous Apache-2.0. Le moteur restic reste sous BSD-2-Clause ; Fyne est sous BSD-3-Clause. Toutes les obligations de redistributions et de licences des dépendances transitives doivent être respectées. Voir [LICENSE](LICENSE), [NOTICE](NOTICE), [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

English: [README.en.md](README.en.md).
