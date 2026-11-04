# Civora — éligibilité anonyme v0.3

Implémentation locale des jalons v0.1 à v0.3 de la [spécification](spec/Protocole-Civique-v1.md). Le noyau Go gère les comptes et la gouvernance. v0.3 ajoute l’arbre d’éligibilité persistant, les instantanés historiques, l’API de chemins et les preuves Semaphore réelles, vérifiées indépendamment en Go et dans le SDK TypeScript.

Pour lancer la démonstration depuis PowerShell :

```powershell
.\scripts\demo.ps1
```

Elle exécute le scénario de gouvernance de **99 blocs**, puis vérifie les deux vecteurs Semaphore de profondeurs **16 et 26**. Le résumé affiche une loi, un rôle, une attribution active et la révision de politique 2, suivis de deux résultats `valid:true`.

La partie gouvernance conserve le **simulateur de développement v0.2**, compilé seulement avec `civora_sim`, dans `civsim`, et refusé par `civnode`. Les preuves Semaphore v0.3 sont réelles. Le chiffrement et le dépouillement ElectionGuard restent à v0.4 : une preuve d’éligibilité seule ne permet pas de compter un vote. Aucune PWA ni réseau de consensus n’est démarré.

La préparation **v0.4** ajoute les décodeurs stricts d’enveloppe ElectionGuard en Go et TypeScript, 59 vecteurs communs et une inspection d’archive locale. **Le jalon reste incomplet** : la sérialisation officielle 0.0.1 laisse le bulletin chiffré et d’autres structures à compléter ; le candidat Kotlin 2.0 consulté utilise surtout une sérialisation 1.9. Le mapping complet, le chiffrement, le dépouillement, les deux vérificateurs cryptographiques et la preuve d’indépendance restent à qualifier. La [décision et les pièces épinglées](spec/decisions/0009-electionguard-interop.md) précisent ce constat et les conditions de reprise. v0.5 n’est pas commencé.

```sh
npm run fixtures:electionguard:check
npm run crypto:electionguard:review
npm run artifacts:electionguard:fetch
npm run crypto:electionguard:gate
go run ./cmd/civcrypto inspect -kind ballot -in spec/fixtures/v0.4/ballot.envelope.json
```

La porte `crypto:electionguard:gate` termine volontairement avec le **code 2** tant que la suite est non qualifiée. Le téléchargement des cinq documents épinglés contrôle leur intégrité ; il ne qualifie pas le chiffrement. L’inspecteur affiche les empreintes et tailles, avec `cryptography_verified:false` et `suite_qualified:false`. Les exemples d’enveloppe sont des marqueurs `TEST_ONLY`, sans contenu ElectionGuard valide ; ils ne permettent aucun vote secret. L’[annexe](spec/interop/electionguard-v0.4.md) décrit la frontière exacte.

Les empreintes des WASM, zkey et clés de vérification sont figées dans [le verrou Semaphore](spec/crypto/semaphore-v4.lock.json). Le SDK est fixé à **4.14.5**, avec ses circuits **4.13.0**. Les navigateurs contrôlent taille et SHA-256 avant chargement ; Go utilise les clés de vérification intégrées et contrôlées. Aucun votant ne choisit ses artefacts.

Prérequis : Go ≥ 1.26 et Node ≥ 24.12, npm. Les versions et dépendances utilisées sont consignées dans [le verrou de développement](spec/profiles/development.lock.json), `go.sum` et `package-lock.json`.

```sh
npm ci --ignore-scripts
npm test
go test ./... -count=1 -timeout 120s
go vet ./...
go test -tags civora_sim ./... -count=1 -timeout 180s
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

Sous Windows, [scripts/check.ps1](scripts/check.ps1) exécute les vérifications, les deux modes de compilation, les contrôles de reproductibilité, les démonstrations, la projection de l’index et leur reprise. Les scripts utilisent Go installé dans `.tools/go` si disponible.

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

Les tests couvrent les transitions de gouvernance, les bornes temporelles, les quorums, les conflits, les capacités, les chemins forgés et les preuves d’un autre réseau, scrutin ou contenu. **Vingt-huit scénarios distincts d’arrêt forcé** couvrent comptes, activation, bulletins, effets, écritures du registre avec arbre d’éligibilité et transactions du stockage de versions. La reprise doit retrouver les mêmes états, racines et chemins historiques.

Le TypeScript conserve l’oracle des comptes v0.1, vérifie les schémas et racines d’état, et utilise le SDK officiel comme oracle LeanIMT/Semaphore. Il ne réimplémente pas la machine de gouvernance ; l’ancien oracle refuse les genèses avec gouvernance ou arbre d’éligibilité.

Les [fixtures](spec/fixtures/) contiennent **des clés privées publiques de test**, explicitement étiquetées `TEST_ONLY`. `npm run fixtures` régénère v0.1 ; `go run -tags civora_sim ./cmd/civsim fixtures` régénère v0.2. Pour v0.3 : `npm run fixtures:eligibility`, puis `go run ./cmd/civeligibility fixtures`. Les identités et arbres sont déterministes ; les preuves Groth16 figées sont réutilisées et vérifiées. Leur régénération probabiliste exige explicitement `--new-proofs`. Les modes `--check` / `-check` ne réécrivent rien. `npm run schemas` régénère les schémas partagés.

Les décisions sont consignées dans [spec/decisions](spec/decisions/0007-semaphore-v0.3.md), avec les formats de preuve, la persistance et les limites de qualification. Les ports `EligibilityProvider`, `MembershipVerifier`, `VotingCrypto` et `EffectAdapter` restent distincts. L’effet courant est exclusivement `ledger_only_v1`.

Les rotations de validateurs et gardiens, l’approbation et l’armement des mises à jour, CometBFT, la PWA, le déploiement et les outils d’urgence restent aux jalons correspondants. Le stockage local est prévu pour un seul écrivain ; les tests Windows qualifient l’arrêt de processus, pas une coupure électrique.
