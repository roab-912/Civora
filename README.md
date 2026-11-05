# Civora — réseau de développement v0.5

Implémentation de développement des jalons v0.1 à v0.5 de la [spécification](spec/Protocole-Civique-v1.md). Le noyau Go gère comptes, gouvernance, éligibilité Semaphore et vote chiffré ElectionGuard 2.0. CometBFT **0.38.26** réplique le registre et les partitions. Go et TypeScript vérifient les preuves cryptographiques et les en-têtes natifs à quorum de **cinq validateurs sur sept**.

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

Sous Windows, [scripts/check.ps1](scripts/check.ps1) exécute les vérifications, les deux modes de compilation, les contrôles de reproductibilité, les démonstrations, la projection de l'index, les reprises et le réseau natif. `-SkipNetwork` permet un contrôle local partiel. Les scripts utilisent Go installé dans `.tools/go` si disponible.

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

Les rotations et activations testées sont préautorisées par genèse de développement, hors scrutin actif. L'approbation institutionnelle et l'armement coordonné multichaîne restent à qualifier. Les archives sélectives, audits et relais relèvent de v0.6 ; la PWA, le déploiement et les outils d'urgence restent aux jalons suivants. Le stockage local est prévu pour un seul écrivain ; les tests Windows qualifient l'arrêt de processus, pas une coupure électrique.
