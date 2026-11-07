# 0017 — Distribution expérimentale préparant v1.0

Date : 2026-11-07. Statut : implémenté pour les essais locaux, publication publique bloquée.

Le §12 réserve la qualification à dix millions à un rapport v0.9 réussi. Les preuves et audits indépendants manquent encore. Le paquet s'appelle donc **1.0.0-rc.1**, canal `experimental`, capacité vérifiée `null`, publication publique et activation `false`. Aucun réseau ni genèse n'est promu. Le vérificateur de fichiers ne certifie ni la capacité ni une activation.

## Formats et compatibilité

`formats.json` recense les octets, tailles, empreintes et `$id` des schémas existants. Leurs espaces de noms `v0.1`, époques, domaines de signature et vecteur du §13 restent inchangés. La PWA garde sa version 0.8.0 ; son numéro n'est pas celui de l'outil de distribution. Le manifeste externe suit `spec/distribution/release.schema.json`, namespace `distribution/v1`, propriétés inconnues et doublons de fichiers refusés. Les tailles y sont des chaînes décimales.

TUF et la provenance SLSA conservent leurs formats normalisés, y compris leurs nombres JSON. Ils ne sont pas encodés avec le codec canonique Civique. Le catalogue est candidat ; le figer comme format public exige la qualification et une nouvelle revue.

## Chaîne de confiance de distribution

Le module Go `tools/distribution` est séparé du noyau afin de ne pas modifier les versions cryptographiques du vote. Il épingle **go-tuf/v2 v2.4.2** et utilise son parcours de mise à jour officiel, snapshots cohérents, rotations séquentielles et temps de référence fixé à l'ouverture de la vérification. Le miroir est local et non fiable ; aucune requête réseau, extraction ou installation n'est déclenchée par `verify`.

Le profil utilise uniquement Ed25519 : racine 3/5, targets 3/5, snapshot 1/1 et timestamp 1/1, avec clés distinctes entre rôles. Une rotation est validée par l'ancien et le nouveau seuil. Le vérificateur exige la racine initiale installée, son SHA-256 obtenu indépendamment et un état persistant ; il vérifie toutes les cibles SHA-256 et leurs tailles exactes. Il refuse les délégations, noms ambigus et sorties du miroir. Le premier chargement depuis une PWA compromise ne remplace pas ce point de confiance.

Les bornes locales sont 512 000 octets pour une racine, 16 384 pour timestamp, 2 000 000 pour snapshot, 5 000 000 pour targets et 8 Gio par cible. Une taille signée par une clé en ligne ne peut pas supprimer ces bornes. Les sorties de construction restent sous `.tools/` et les compilations utilisent une copie figée des sources sélectionnées ; les fichiers non suivis étrangers à cette préparation sont exclus.

La base bbolt verrouille l'accès concurrent et engage les métadonnées acceptées avant le téléchargement suivant ainsi qu'à la fin du parcours, y compris après un échec ultérieur. Chaque essai restaure un cache privé depuis la base ; les caches temporaires abandonnés ne deviennent jamais une racine de confiance. Les versions et l'heure maximale sont conservées après arrêt brutal. Un rollback d'horloge est refusé. L'état local est un élément de confiance à protéger ; sa suppression volontaire retire la mémoire anti-retour. Les tests couvrent l'arrêt du processus, pas la panne physique d'un stockage qui ment sur sa durabilité.

La racine de distribution est séparée de la racine ML-DSA de reprise. La compromission du seuil TUF n'autorise aucun reset depuis le miroir ; le chemin de reprise indépendant reste celui du §15.4. La signature d'une cible ne donne aucun pouvoir au registre.

## Construction et clés d'exercice

`seal-test` n'accepte que l'usage explicite de clés publiques `PUBLIC_TEST_ONLY`. Les graines Ed25519 sont le SHA-256 de `CIVORA/DISTRIBUTION/PUBLIC_TEST_ONLY/1/<rôle>/<siège>`. Il s'agit de matériel compromis par conception. Le vérificateur le refuse par défaut, même si le marqueur de la racine est retiré pour les clés standard. Ce programme ne crée aucune clé d'exploitation. Les durées locales sont un an, sept jours et 24 heures ; elles sont mesurées depuis l'epoch explicitement fourni.

Les ZIP utilisent ordre lexical, timestamp fixe 1980-01-01, modes fixes, chemins restreints et fichiers ordinaires. Les Go binaires utilisent `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`, `-ldflags=-buildid=`. Les dépendances npm et artefacts de preuve sont épinglés. Une liste de sources et une liste de contenus publics excluent magasins locaux, TLS et secrets d'exploitation. L'archive source inclut les fixtures de test identifiées ; elles ne doivent jamais devenir des clés réelles.

Les auditeurs Node sont regroupés avec leurs dépendances. Le composant épinglé `web-worker` reste un fichier CommonJS distinct, livré sous `tools/runtime/`, car il démarre son propre fichier dans un thread. Le paquet s'utilise avec son arborescence complète et sans installation npm supplémentaire ; son test d'acceptation vérifie une preuve Semaphore réelle hors du dépôt source.

La provenance est une déclaration in-toto v1 avec prédicat SLSA provenance v1, empreinte de l'archive source, verrous, outils, paramètres et produits. C'est une auto-attestation locale sans niveau SLSA. L'inventaire des modules Go/npm est fourni séparément. Une égalité de deux constructions locales est utile à la reproductibilité, mais n'est pas une preuve de deux environnements indépendants. La revue de deux personnes indépendantes et la cérémonie de publication hors ligne restent bloquantes ; `--public` ne possède aucun chemin d'acceptation dans ce candidat.

Références : [TUF](https://github.com/theupdateframework/specification/blob/master/tuf-spec.md), [go-tuf v2.4.2](https://github.com/theupdateframework/go-tuf/releases/tag/v2.4.2), [SLSA provenance](https://slsa.dev/spec/v1.2/build-provenance), [manuel](../manual/README.md).
