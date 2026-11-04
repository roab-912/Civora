# Civora — noyau v0.1

Implémentation locale du [jalon v0.1](spec/Protocole-Civique-v1.md) : comptes Ed25519, nonces, JSON strict, arbre de Merkle clairsemé, registre de suites, formats de plans et rejeu durable. Go exécute le registre ; TypeScript fournit une implémentation indépendante pour les vecteurs, les transitions et les racines.

Prérequis : Go ≥ 1.26 et Node ≥ 24.12, npm. Les versions et dépendances utilisées sont consignées dans [le verrou de développement](spec/profiles/development.lock.json), `go.sum` et `package-lock.json`.

```sh
npm ci --ignore-scripts
npm test
go test ./... -count=1 -timeout 120s
go vet ./...
go run ./cmd/civnode replay -fixture spec/fixtures/accounts.json -store .dev-store/accounts
go run ./cmd/civnode replay -fixture spec/fixtures/accounts.json -store .dev-store/accounts
go run ./cmd/civnode replay -fixture spec/fixtures/activation.json -store .dev-store/activation -supported-epoch 2
```

La deuxième commande de rejeu reprend les blocs déjà durables et vérifie leur identité. Sans `-supported-epoch 2`, le scénario d’activation s’arrête avant le bloc 3. La CLI accepte uniquement la commande locale `replay` et des genèses `mode:development`, `voting_profile:inline_dev`.

Sous Windows, [scripts/check.ps1](scripts/check.ps1) exécute les vérifications et les démonstrations dans un nouveau répertoire ignoré. Il utilise aussi Go installé dans `.tools/go` si disponible.

Les tests couvrent le vecteur exact du §13 dans les deux langages, les racines et événements partagés, les encodages ambigus, les clés réservées, les autorisations de récupération, les nonces concurrents, les contextes étrangers, les suites révoquées et l’arrêt forcé d’un processus à cinq points d’écriture. Le stockage persiste ensemble bloc, état, événements et hauteur. Au redémarrage, il rejoue et vérifie chaque enregistrement.

Les [fixtures](spec/fixtures/) contiennent **des clés privées publiques de test**, explicitement étiquetées `TEST_ONLY`. `npm run fixtures` les régénère à l’identique ; les tests contrôlent leur reproductibilité. `npm run schemas` régénère les schémas JSON Schema 2020-12, partagés par les deux langages.

Les formats qui complètent la RFC sont documentés dans [spec/decisions](spec/decisions/0001-encodage-et-profil.md). Le format v2 des transactions sert uniquement à exercer le changement d’epoch sans modifier le vecteur v1. Les plans préarmés et leurs empreintes d’artefacts sont des données de test, sans autorité de publication ou de gouvernance.

Ce jalon ne démarre aucun réseau. CometBFT, la gouvernance, Semaphore, ElectionGuard, la PWA, le déploiement et les outils d’urgence restent aux jalons suivants. Les interfaces d’instantané, de transport de bulletins, de preuve de résultat, de stockage et de version sont isolées dans `internal/ports` et `web/src/ports.ts`. Le stockage local est prévu pour un seul écrivain ; les tests Windows qualifient l’arrêt de processus, pas une coupure électrique.
