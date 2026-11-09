# Civora — distribution expérimentale 1.0.0, PWA v0.8

Édition expérimentale 1.0.0 : archives Windows/Linux, formats, profils, outils d’audit autonomes, inventaire et provenance SLSA, vérification TUF avec état anti-retour persistant. **Le pilote public reste bloqué par v0.9 ; aucune capacité à dix millions n’est revendiquée.** Le [manuel de distribution](spec/manual/README.md) donne les commandes ; la [décision 0017](spec/decisions/0017-distribution-v1.0.md) fixe les choix d’interopérabilité. La [décision 0018](spec/decisions/0018-finalisation-experimentale-v1.0.md) décrit la reconstruction sans Git et la finalisation liée aux tests.

```sh
./scripts/check.ps1 -Distribution -Report .tools/v1.0-checks.json
npm run release:build -- --epoch 2026-11-07T12:00:00Z --out .tools/releases/v1.0
npm run release:finalize -- --release .tools/releases/v1.0 --checks .tools/v1.0-checks.json --output spec/reports/v1.0-distribution.json --allow-test-keys
```

Implémentation de développement des jalons v0.1 à v0.8 de la [spécification](spec/Protocole-Civique-v1.md). Le noyau Go gère comptes, gouvernance, éligibilité Semaphore et vote chiffré ElectionGuard 2.0. CometBFT **0.38.26** réplique le registre et les partitions. Go et TypeScript vérifient les preuves cryptographiques et les en-têtes natifs à quorum de **cinq validateurs sur sept**.

V0.9 ajoute le contrôle de reçus natifs sous charge, les prérequis de reprise hors consensus en **ML-DSA-65/SHA3-384**, la persistance des compteurs et rotations, les suspensions prioritaires dans une genèse de laboratoire, le fuzzing et un rapport de campagne bloquant. **Le jalon de qualification reste incomplet** : campagne réelle de dix millions de bulletins sur 32 partitions, audits externes, appareils physiques et cycle complet de reprise restent nécessaires. L’atelier et sa PWA restent en développement v0.8 ; leur genèse n’est pas modifiée.

```sh
npm run test:qualification
npm run qualify -- --output .tools/qualification-pending.json
```

La deuxième commande retourne **2** avec le dossier d’exemple : les preuves requises sont absentes, le pilote reste non qualifié. Le [guide v0.9](spec/qualification/README.md) décrit les mesures, la confiance et les exercices, la [décision 0015](spec/decisions/0015-qualification-v0.9.md) fixe les formats et les limites, et le [rapport v0.9](spec/reports/v0.9-qualification.json) distingue les contrôles locaux et les critères non satisfaits.

Pour ouvrir la PWA sur **http://127.0.0.1:8790** :

```sh
npm ci --ignore-scripts
npm run artifacts:fetch
npm run dev
```

Le menu **Application / Réseau** sépare les usages : propositions, votes, lois, rôles et compte dans Application ; activité des nœuds, téléchargements, documentation et commandes de l’atelier dans Réseau. **Vérifier le réseau** propose l’observateur existant ; **Explorateur** affiche les blocs et les états publics avec leurs contrôles cryptographiques ; **Vérifier un reçu** contrôle un reçu individuel (`civora-recu.json`) ou un rapport de résultat (`civora-verification.json`), en revérifiant les preuves du registre. **Participer au consensus** présente uniquement une maquette, avec actions désactivées. Un seul réseau est proposé ; l’ancienne archive v0.6 reste uniquement dans les fixtures de test.

Pour lire et vérifier le réseau depuis un autre ordinateur, ouvrir **Réseau → Vérifier le réseau**, télécharger l’archive Windows ou Linux et l’extraire. Dans ce dossier, lancer `.\civora-reseau.exe` sous Windows, ou `chmod +x civora-reseau && ./civora-reseau` sous Linux. Aucun Go ou Node.js n’est nécessaire sur ce poste. Le programme vérifie et conserve une copie du registre ; `Ctrl+C` l’arrête et la même commande le reprend. Il ne participe pas au consensus. Les compteurs distinguent le serveur, les observateurs déclarés actifs et les comptes citoyens, sans compter les gardiens de démonstration comme des personnes. Le [guide du réseau](spec/network/README.md) détaille le téléchargement, la confiance, la reprise et les limites.

Go ≥ 1.26 et Node ≥ 24.12 sont nécessaires. Le lanceur utilise `.tools/go` s'il existe. Les artefacts Semaphore sont épinglés et contrôlés avant chaque chargement ; après leur premier téléchargement, le parcours fonctionne sans service externe. `npm run dev -- --port 8791` choisit un autre port. Les boutons **Atelier** font avancer explicitement le temps du registre, ouvrent, dépouillent et appliquent les décisions. Chaque lancement crée une session isolée dans `.tools/pwa/sessions/`, avec un identifiant réseau distinct ; les comptes et votes d’un atelier précédent restent associés à cet ancien réseau. `Ctrl+C` arrête les processus de cette session. La barre de l’interface affiche la version de distribution expérimentale ; le format de configuration PWA reste en 0.8.0.

L’atelier utilise **des clés publiques TEST_ONLY et des commits synthétiques signés**. Les transitions du noyau, le chiffrement et les preuves sont réels. Un observateur rejoint cet atelier par HTTP(S), vérifie ses blocs et conserve une archive locale ; il ne devient pas automatiquement validateur. Cette démonstration ne constitue pas un réseau de production ni une qualification du pilote public.

Depuis un autre ordinateur du même Wi-Fi, Web Crypto nécessite **HTTPS** : une adresse `http://192.168.…` ne permet pas de créer un coffre ni de chiffrer un bulletin. Après `npm run dev`, démarrer dans un autre terminal, avec l’adresse IPv4 Wi-Fi de ce PC et OpenSSL installé (celui de Git pour Windows est détecté) :

```sh
npm run dev:lan -- --host 192.168.1.40
```

Ouvrir **http://192.168.1.40:8790/__lan/** sur l’autre poste. Télécharger le certificat local ; sous Windows, ouvrir le fichier et choisir « Installer le certificat » → « Utilisateur actuel » → « Autorités de certification racines de confiance ». Fermer puis rouvrir le navigateur et utiliser **https://192.168.1.40:8791**. Firefox peut demander l’import dans les autorités de son propre gestionnaire de certificats. Le certificat et les clés TLS sont créés dans `.tools/pwa/lan-tls/`, sans installation automatique dans le système. Seul le certificat public est téléchargeable. L’empreinte de l’autorité est affichée dans le terminal du PC serveur, à comparer avec le certificat reçu. Ce réglage concerne l’atelier sur le réseau local ; l’hébergement public reste hors périmètre.

Le pont conserve la session du serveur sur `127.0.0.1:8790`. Il écoute uniquement sur l’adresse privée choisie, accepte le sous-réseau local, refuse les POST d’autres origines et redirige les lectures HTTP vers HTTPS. Son port HTTP sert la page d’installation du certificat. Les comptes enregistrés dans le navigateur restent propres à leur origine : pour retrouver un compte d’une autre adresse, importer sa sauvegarde chiffrée et son code. Contrôle ciblé, sans inscription supplémentaire : `npm run test:pwa:lan -- --url https://192.168.1.40:8791`.

La PWA permet de créer un compte, télécharger sa sauvegarde chiffrée et son code séparé, récupérer le compte sur un autre navigateur, tourner ou retirer ses clés, proposer et soutenir une décision, voter, télécharger et vérifier son reçu, contrôler le résultat, consulter les révisions des lois et gérer les rôles par de nouveaux votes. Les calculs cryptographiques s’exécutent dans un worker. Hors ligne, les données publiques déjà consultées portent la date de synchronisation ; aucun vote n’est envoyé automatiquement au retour de la connexion.

L’onglet **Actualités** présente les ouvertures de scrutin, sans compte et même si les notifications sont refusées. Dans **Mon compte** ou au bas des actualités, choisir les réseaux puis cliquer sur **Activer les notifications** : aucune autorisation n’est demandée à l’ouverture de la page. Le message sur l’écran verrouillé reste générique : « Nouveau référendum. Consulter la question ». Son clic ouvre le dossier du bon réseau, sans envoyer de vote. Les préférences et la désinscription sont accessibles au même endroit. Sur iPhone/iPad compatible, installer d’abord l’application par **Partager → Sur l’écran d’accueil** ; les actualités restent lisibles sans installation.

Le serveur de notifications conserve abonnements, curseurs et tâches dans une base SQLite distincte du registre. Il contrôle les preuves des ouvertures finalisées, reprend après arrêt, réessaie les erreurs transitoires jusqu’à la clôture et supprime les abonnements invalides. Les clés civiques et les bulletins ne lui sont pas transmis. Un fournisseur Push indisponible ne bloque pas la lecture des actualités. La clé VAPID est générée localement et reste dans le répertoire privé du service ; ne pas publier ce répertoire. Pour de vrais envois, renseigner le contact opérateur, par exemple `npm run dev -- --push-contact mailto:contact@example.org` ou `CIVORA_PUSH_CONTACT`. Le contact par défaut est réservé au développement. Les services Push du navigateur nécessitent un accès Internet ; les actualités utilisent le réseau local.

Pour conserver un atelier déjà lancé pendant la mise à jour de son interface :

```sh
npm run dev:attach -- --upstream http://127.0.0.1:8790 --port 8794
npm run dev:lan -- --host 192.168.1.40 --upstream http://127.0.0.1:8794
```

Arrêter l’ancien pont Wi-Fi avant de relancer le second ; conserver le serveur d’atelier. Le nouveau pont réutilise le certificat local et la même adresse HTTPS, donc le coffre du navigateur reste sous la même origine. Fermer les anciens onglets puis rouvrir l’application termine la mise à jour du service worker. L’interface v0.8 est construite dans `.tools/pwa/v0.8/public`, séparément des assets de l’ancienne session.

Contrôle ciblé : `npm run test:notifications`. Il vérifie les vecteurs RFC 8291, VAPID, les refus de destinations internes, les doublons, les délais, le désabonnement, SQLite après arrêt brutal et le parcours dans Chromium complet et WebKit en émulation iPhone. Les abonnements et le fournisseur sont **TEST_ONLY** dans ce test ; aucun Push n’est envoyé à un service externe. L’installation sur des iPhone/iPad physiques et la livraison par les fournisseurs réels restent à qualifier. Voir la [décision 0014](spec/decisions/0014-notifications-v0.8.md) et le [rapport v0.8](spec/reports/v0.8-notifications.json).

Pour utiliser des nœuds existants, authentifier leur manifeste puis créer un fichier de configuration à partir de [pwa-networks.example.json](pwa-networks.example.json) :

```sh
npm run pwa:host -- --config pwa-networks.example.json
```

Ce serveur d’interface écoute sur **http://127.0.0.1:8792** et relaie les API configurées ; il ne lance aucun gardien ni contrôle d’atelier. L’exemple utilise les deux archives du lanceur `citizen.ps1` ci-dessous. Leur état figé est signalé comme périmé et les écritures restent bloquées. La genèse configurée est le point de confiance initial, à comparer par un canal indépendant. L’hébergement public, HTTPS et la distribution signée relèvent des jalons ultérieurs.

Acceptation PWA : `npm run browsers:install`, puis `npm run test:pwa`. Le test exécute le parcours complet dans Chromium sur ordinateur et WebKit en émulation iPhone, avec import de sauvegarde entre les deux moteurs, six votes réels, lois et rôles, refus des contextes étrangers, reçu et cache hors ligne. Les captures et rapports locaux restent dans `.tools/qualification/pwa-*`. Les [choix d’interopérabilité](spec/decisions/0013-pwa-v0.7.md) et le [rapport v0.7](spec/reports/v0.7-pwa.json) précisent les limites : appareils physiques et audit externe non qualifiés, absence de garantie de temps constant ou d’effacement des secrets JavaScript. Les notifications v0.8 sont décrites ci-dessus.

Pour lancer l'hôte citoyen avec deux archives locales et une réplique anonyme du registre et de la partition 0 :

```powershell
.\scripts\citizen.ps1
# Ou lancer, vérifier le rejeu et l'export, puis arrêter les processus :
.\scripts\citizen.ps1 -Check
```

API de l'observateur : **http://127.0.0.1:8788/v1/network**. `Ctrl+C` conserve les archives dans `.dev-host`. La démonstration emploie de vrais chiffrés et preuves, des clés **PUBLIC_TEST_ONLY** et des commits synthétiques liés. Elle ne constitue pas un réseau de production. Les reçus distinguent dépôt répliqué et comptage ; le dossier sélectif est revérifié séparément en Go et TypeScript. La fixture certifiée compte **3 Oui et 2 Non**, attend la fenêtre d'audit puis applique la loi.

Les interfaces v0.6 sont utilisables directement avec un manifeste authentifié :

```sh
go build -o .tools/civnode.exe ./cmd/civnode
.tools/civnode.exe init --home .dev-host/operator --manifest network.json --genesis genesis.json
.tools/civnode.exe start --home .dev-host/operator --mode archive --lanes 0
.tools/civnode.exe status --home .dev-host/operator
.tools/civnode.exe verify --home .dev-host/operator --from genesis
.tools/civnode.exe election export --home .dev-host/operator --poll-id ID --output election-record
.tools/civnode.exe election verify --home .dev-host/operator --input election-record
npm run audit:citizen -- -input election-record -manifest network.json
```

`start` utilise CometBFT/P2P par défaut. `--transport http` utilise les archives signées du manifeste ; les adresses P2P de la fixture figée sont des exemples. L'hôte public ne nécessite aucun compte ni clé privée de consensus. Une fois arrêté, `status` et `verify` peuvent relire l'archive locale hors ligne et signalent son caractère périmé. Les origines du manifeste doivent correspondre au réseau effectivement lancé.

Les services d'archive et les travailleurs de relais sont documentés dans la [décision 0012](spec/decisions/0012-citizen-v0.6.md). Pour le développement, une archive reçoit explicitement `--archive-key chemin/archive-N.TEST_ONLY.json` ; un relais reçoit `--archives http://archive-a,http://archive-b,http://archive-c`. Les clés d'audit restent séparées. La certification exige **3/5 auditeurs et les deux implémentations**, trente minutes de contestation et la clôture du dossier sous 48 heures. Les clés publiques de test, la publication de binaires, le déploiement, l'outillage d'urgence et la qualification à dix millions restent hors du pilote public.

Contrôle complet : `./scripts/check.ps1`. Test natif v0.6 seul : `npm run test:citizen:native` (quatorze validateurs locaux et un observateur, arrêt brutal/reprise). Vérification des fixtures : `go run ./cmd/civnode fixtures -check -certified`. Le [rapport v0.6](spec/reports/v0.6-citizen.json) consigne les résultats et limites, dont l'arrêt gracieux natif restant à qualifier. Les choix de preuve **état H/en-tête H+1**, d'ordre des sœurs et de confiance initiale sont consignés dans la décision 0012.

Pour lancer sept nœuds locaux depuis PowerShell :

```powershell
.\scripts\network.ps1
```

Le statut du premier nœud est accessible sur `http://127.0.0.1:27651/status`. Les clés sont **publiques et réservées aux tests**. `Ctrl+C` arrête les processus en conservant leurs bases et états de signature dans `.dev-network`. Une nouvelle exécution reprend ces états. Les ports et le répertoire se choisissent avec `-Port` et `-Directory` à la première initialisation ; les adresses enregistrées restent celles de cette initialisation.

Le test d'acceptation lance de vrais processus CometBFT, vérifie finalité, perte de quorum, reprise, rotation des clés à H+2 et activation avec plusieurs versions :

```sh
npm run test:consensus
```

Le corpus de huit partitions compte cinq bulletins réels et rejette une copie dotée d'une vraie preuve Semaphore. Il exige les scellés de toutes les partitions, même vides, et refuse un lot manquant. Le profil `transport_dev_v0_5` produit un résultat **provisoire**, sans effet de loi avant certification d'audit. L'absence de certification à 48 heures mène à `failed_certification`. La [décision 0011](spec/decisions/0011-consensus-partitions-v0.5.md) précise formats, budgets, barrières et limites. Les commits fabriqués des fixtures sont explicitement distingués des commits issus du test réseau natif. Le pilote public et la capacité à dix millions de bulletins restent non qualifiés.

Le [rapport v0.5](spec/reports/v0.5-acceptance.json) consigne les contrôles exécutés, les racines reproductibles et les éléments différés.

Pour lancer la démonstration depuis PowerShell :

```powershell
.\scripts\demo.ps1
```

Elle exécute le scénario de gouvernance simulée de **99 blocs**, vérifie les vecteurs Semaphore **16 et 26**, puis rejoue un véritable scrutin ElectionGuard de **9 blocs**, exporte son registre public et le vérifie sans la base d’origine. Le scrutin chiffré compte trois Oui et deux Non et applique la loi dans le registre.

Le simulateur historique v0.2 reste compilé seulement avec `civora_sim`, dans `civsim`, et refusé par les nœuds. Les scénarios v0.4 utilisent le profil réel `electionguard_v0_4`, de vrais bulletins du SDK verrouillé et de vraies preuves Semaphore. La commande `demo.ps1` effectue des rejeux locaux ; `network.ps1` démarre le réseau.

Le blocage initial de sérialisation est résolu par une [annexe Civic explicite](spec/interop/electionguard-v0.4.md), conservant le design 2.0.0 et complétant les structures ouvertes de la référence 0.0.1. Le format compact, les sources, les artefacts et les vecteurs sont [verrouillés](spec/crypto/electionguard-v2.lock.json). Le SDK Kotlin épinglé a été compilé et exécuté ; son format n’est pas présenté comme le format officiel 0.0.1. La [décision 0010](spec/decisions/0010-electionguard-v0.4.md) détaille ce choix.

```sh
npm run fixtures:electionguard:check
npm run crypto:electionguard:review
npm run artifacts:electionguard:fetch
npm run crypto:electionguard:gate
go run ./cmd/civcrypto inspect -kind ballot -in spec/fixtures/v0.4/ballot.envelope.json
go run ./cmd/civnode replay -fixture spec/fixtures/v0.4/replay-mixed.json -store .dev-store/encrypted
go run ./cmd/civcrypto export -store .dev-store/encrypted -out .dev-store/public-encrypted.json
go run ./cmd/civcrypto audit -in .dev-store/public-encrypted.json
npm run crypto:electionguard:audit -- -in .dev-store/public-encrypted.json
```

La porte `crypto:electionguard:gate` contrôle le verrou et revérifie les chiffrés, les totaux et le refus de copie sous un autre nullifier. `qualified:false` signifie que le pilote public reste non qualifié ; `development_interop_verified:true` distingue le résultat de développement. L’inspecteur seul garde `cryptography_verified:false`. Les anciens fichiers `*.envelope.json` restent des marqueurs de transport sans contenu cryptographique valide ; les vraies fixtures sont `crypto.json` et `replay-*.json`.

Pour reconstruire l’oracle depuis l’archive du commit SDK exact, avec Java 17 et Gradle confinés à `.tools` : `./scripts/electionguard-oracle.ps1 -Fetch`. Les exécutions suivantes peuvent utiliser le cache hors ligne sans `-Fetch`. Les preuves de total figées sont revérifiées, sans changement de leurs octets. Le [manuel du harnais](tools/electionguard/README.md) précise la régénération explicite.

Le vérificateur pur v0.4 conserve son contexte de confiance explicite. Le pilote v0.5 authentifie l'ouverture et les points de contrôle par en-têtes signés et preuves d'état, persiste admission, traitement et nullifiers, puis vérifie le seul déchiffrement global. La cérémonie de développement reste trois gardiens sur cinq. Le profil public `scale_1`, son audit externe et sa capacité de masse restent à qualifier.

Les empreintes des WASM, zkey et clés de vérification sont figées dans [le verrou Semaphore](spec/crypto/semaphore-v4.lock.json). Le SDK est fixé à **4.14.5**, avec ses circuits **4.13.0**. Les navigateurs contrôlent taille et SHA-256 avant chargement ; Go utilise les clés de vérification intégrées et contrôlées. Aucun votant ne choisit ses artefacts.

Prérequis : Go ≥ 1.26 et Node ≥ 24.12, npm. Les versions et dépendances utilisées sont consignées dans [le verrou de développement](spec/profiles/development.lock.json), `go.sum` et `package-lock.json`.

```sh
npm ci --ignore-scripts
npm test
go test ./... -count=1 -timeout 300s
go vet ./...
go test -tags civora_sim ./... -count=1 -timeout 300s
go vet -tags civora_sim ./...
go run ./cmd/civnode replay -fixture spec/fixtures/accounts.json -store .dev-store/accounts
go run ./cmd/civnode replay -fixture spec/fixtures/accounts.json -store .dev-store/accounts
go run ./cmd/civnode replay -fixture spec/fixtures/activation.json -store .dev-store/activation -supported-epoch 2
go run -tags civora_sim ./cmd/civsim demo
go run -tags civora_sim ./cmd/civsim replay -fixture spec/fixtures/v0.2/governance.json -store .dev-store/governance
go run ./cmd/civnode replay -fixture spec/fixtures/v0.3/accounts.json -store .dev-store/eligibility-ledger
go run ./cmd/civeligibility index -ledger .dev-store/eligibility-ledger -store .dev-store/eligibility-index
go run ./cmd/civeligibility path -store .dev-store/eligibility-index -height 2 -index 1
go run ./cmd/civeligibility serve -store .dev-store/eligibility-index -addr 127.0.0.1:8783
```

Un rejeu répété sur le même répertoire reprend les blocs durables et vérifie leur identité. Sans `-supported-epoch 2`, le scénario d’activation s’arrête avant le bloc 3. Les CLI acceptent uniquement les genèses `mode:development`, `voting_profile:inline_dev`. Les anciens scénarios v0.1 conservent leurs racines.

Sous Windows, [scripts/check.ps1](scripts/check.ps1) exécute les vérifications, les deux modes de compilation, les contrôles de reproductibilité, les démonstrations, la projection de l'index, les reprises, le réseau natif et la PWA. `-SkipNetwork` et `-SkipPWA` permettent des contrôles partiels. Les scripts utilisent Go installé dans `.tools/go` si disponible.

Pour les preuves dans un Web Worker sur Chromium et WebKit :

```sh
npm run artifacts:fetch
npm run browsers:install
npm run test:browser
```

Pour reconstruire l’arbre à **10 000 000 de feuilles actives**, exercer rotation/retrait et reprise, puis générer les preuves depuis le seul chemin HTTP dans les deux moteurs :

```sh
npm run test:scale
```

Cette vérification crée environ 640 Mo de niveaux d’arbre dans un nouveau répertoire de `.tools/qualification/`. Elle ne supprime pas les anciens essais. Les rapports restent dans `.tools/qualification/`. Les équivalents PowerShell sont `./scripts/check.ps1 -Browsers` et `./scripts/check.ps1 -Scale`.

Les tests navigateur utilisent une émulation mobile. La validation sur deux navigateurs de téléphones physiques reste à effectuer et n’est pas assimilée à cette émulation. Le rapport v0.3 distingue les critères exécutés et ce critère matériel restant.

L’API locale expose uniquement `GET /v1/eligibility/snapshots/latest`, `GET /v1/eligibility/snapshots/{height}` et `GET /v1/eligibility/snapshots/{height}/paths/{index}`. Elle ne fournit ni liste complète de membres ni route d’écriture. Le client doit comparer l’instantané et sa racine au contexte de registre de confiance ; HTTP et `snapshot_id` seuls ne prouvent pas la finalité.

Les tests couvrent gouvernance, quorums, conflits, éligibilité et altérations cryptographiques. **Trente-quatre scénarios distincts d’arrêt forcé** couvrent les essais antérieurs et six écritures de cérémonie, vote et dépouillement réels de part et d’autre de la frontière durable. La reprise retrouve les mêmes états, racines et chemins historiques. Les 59 vecteurs d’enveloppe et 38 refus cryptographiques sont partagés par Go et TypeScript.

Le TypeScript conserve l’oracle des comptes v0.1, vérifie les schémas et racines d’état, et utilise le SDK officiel comme oracle LeanIMT/Semaphore. Il ne réimplémente pas la machine de gouvernance ; l’ancien oracle refuse les genèses avec gouvernance ou arbre d’éligibilité.

Les [fixtures](spec/fixtures/) contiennent **des clés privées publiques de test**, explicitement étiquetées `TEST_ONLY`. `npm run fixtures` régénère v0.1 ; `go run -tags civora_sim ./cmd/civsim fixtures` régénère v0.2. Pour v0.3 : `npm run fixtures:eligibility`, puis `go run ./cmd/civeligibility fixtures`. Les identités et arbres sont déterministes ; les preuves Groth16 figées sont réutilisées et vérifiées. Leur régénération probabiliste exige explicitement `--new-proofs`. Les modes `--check` / `-check` ne réécrivent rien. `npm run schemas` régénère les schémas partagés.

Pour v0.5 : `npm run fixtures:partitions:check`, puis `go run ./cmd/civnet fixtures -check`. La première commande vérifie les preuves figées, la seconde les en-têtes, certificats, blocs et racines déterministes. Les commandes sans `:check` / `-check` régénèrent les fichiers ; un nouveau tirage Groth16 exige explicitement `--new-proofs`.

Les décisions sont consignées dans [spec/decisions](spec/decisions/0011-consensus-partitions-v0.5.md), avec formats de preuve, persistance et limites de qualification. Les ports `EligibilityProvider`, `MembershipVerifier`, `VotingCrypto` et `EffectAdapter` restent distincts. L'effet inline courant est `ledger_only_v1` ; le résultat partitionné reste provisoire. Les archives n'incluent ni les fichiers `TEST_ONLY`, ni les nonces, ni les choix individuels. La genèse doit être authentifiée ; un export local n'est pas une preuve de consensus.

Les rotations et activations testées sont préautorisées par genèse de développement, hors scrutin actif. L'approbation institutionnelle et l'armement coordonné multichaîne restent à qualifier. Les archives sélectives, audits et relais sont implémentés en v0.6 et la PWA en v0.7. Les notifications sont implémentées en v0.8 ; le déploiement et les outils d'urgence restent aux jalons suivants. Le stockage local est prévu pour un seul écrivain ; les tests Windows qualifient l'arrêt de processus, pas une coupure électrique.
