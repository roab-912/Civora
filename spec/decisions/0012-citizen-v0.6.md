# 0012 — Hôte citoyen, archives et audits

Date : 2026-11-05. Portée : **v0.6 de développement**, sans qualification `scale_1`, distribution publique, PWA ni outillage d'urgence. Complète [0011](0011-consensus-partitions-v0.5.md).

## Confiance et réplication

Le manifeste strict `civic-network-1` fixe réseau, profil, identifiants de chaîne, SHA-256 et tailles des octets de chaque genèse native, au moins deux origines HTTP distinctes et les pairs initiaux. L'opérateur doit obtenir ce manifeste par un canal authentifié. L'export ne peut substituer sa propre genèse à cette empreinte. Des URL distinctes ne démontrent pas l'indépendance organisationnelle des exploitants.

`civnode init`, `start --mode archive --lanes 0`, `status`, `verify --from genesis`, `election export` et `election verify` constituent l'interface. Le transport par défaut réplique CometBFT 0.38.26 ; `--transport http` rejoue les mêmes blocs signés avec bascule entre origines. Le statut indique les hauteurs, la dernière heure de bloc, l'epoch et le caractère périmé des données. Le profil de développement n'autorise que l'archive intégrale ; l'élagage est refusé explicitement.

Le nœud public ne crée aucune clé de compte ni clé privée de consensus. CometBFT exige un port `PrivValidator` même pour un observateur : `NoSigner` expose uniquement l'identité **publique P2P non validatrice** ; ses deux opérations de signature échouent toujours. La clé privée P2P authentifie les connexions et n'est jamais utilisée pour voter.

Le rejeu part de la genèse, contrôle les signatures natives 5/7, les corps et liens de blocs, les jeux de validateurs préarmés, les paramètres, l'ordre des hauteurs et l'AppHash antérieur, puis réexécute la machine. Le mode natif bénéficie en outre de la validation complète du moteur CometBFT. Les archives synthétiques sont explicitement identifiées : elles ne prétendent pas constituer un exercice de temps BFT ou de partition WAN.

## Preuves et index

Les preuves publiques `citizen_state_proof` authentifient l'état **après H** par l'AppHash de **H+1**. `state_witness` comporte présence/absence explicite et exactement **256 sœurs feuille vers racine**. Une absence utilise une valeur `null` et le haché de feuille vide. Les anciennes preuves internes de points de contrôle v0.5 conservent leur ordre racine vers feuille ; la conversion est explicite.

Une preuve native de transaction utilise le `DataHash` du bloc **H**, sans décalage. La feuille Merkle native engage `SHA256(transaction)` avant le préfixe de feuille ; appliquer directement RFC 9162 aux octets de transaction serait incompatible. Les signatures natives gardent les règles ZIP-215 épinglées en 0011 ; les attestations Civic utilisent Ed25519/RFC 8032.

L'index bbolt est une projection reconstruite par rejeu. Un catalogue immuable de références de blocs vérifiés, séparé sous `archives/{chain_id}`, conserve les objets SHA-256. Effacer le seul `index/{chain_id}/index.db` ne supprime pas l'archive. Le démarrage hors ligne rejoue ce catalogue, sans instantané de confiance ni clé privée. Un bloc annoncé mais absent/corrompu produit une erreur. Les données ne sont publiées qu'après leur persistance.

Les curseurs de listes fixent hauteur, dernier identifiant, ressource et filtre. Leur fenêtre de validité est de **512 hauteurs** ; une expiration retourne `410`. Le flux d'événements utilise `hauteur:position` et ne contient que des transitions finalisées. Les chemins Semaphore utilisent `leaf_index` et retournent le seul chemin nécessaire. Le téléchargement intégral de groupe reste une fonction du profil local v0.3, pas une exigence de téléchargement côté votant v0.6.

## Archives et relais

Les objets sont adressés par SHA-256 et taille, avec plafond de **32 Mio**, publication atomique après synchronisation du fichier et refus de remplacement silencieux d'un objet corrompu. Le téléchargement utilise `Range`/`If-Range`, conserve un préfixe partiel et ne publie le résultat qu'après vérification complète de taille et d'empreinte. Un miroir défaillant ou corrompu peut être remplacé.

Le service optionnel `/v1/archive/batches` relit le contexte authentifié, vérifie l'admission et persiste toutes les enveloppes ainsi que leur manifeste avant de signer. Trois clés d'archive distinctes sur cinq sont requises. Les formats de référence et le marqueur de rétention `persisted-development-v0.5` restent compatibles avec 0011 ; ce marqueur n'est pas une garantie de service public.

Un dépôt de bulletin transmet ses **octets canoniques exacts** ; son identifiant est leur SHA-256. La file locale persistante, limitée à **1024 tâches**, distingue `seen` de `replicated`. Le reçu natif contient l'inclusion du lot, l'enveloppe et son statut `pending`, `counted`, `invalid` ou `duplicate`. Plusieurs relais conservent le même dépôt et sa première hauteur. Une erreur réseau laisse une tâche visible à réessayer, sans inventer de finalité. Les transactions de compte suivent le même principe de file persistante.

Les travailleurs sans autorité ouvrent les partitions avec une preuve du registre, soumettent les préfixes de traitement de quatre enveloppes et publient des points de contrôle avec au plus 64 nouveaux lots. Ils prennent les préfixes intermédiaires nécessaires au rattrapage. La machine de consensus refait tous les contrôles ; un verdict de travailleur n'est jamais une preuve. La clôture est contrôlée à l'admission finale. Il n'existe pas de renvoi automatique sous une nouvelle identité de scrutin.

## Dossier, certification et contestations

Le profil explicite **`transport_dev_v0_6`** ajoute à la configuration et au contexte figé cinq clés d'audit distinctes des autres rôles. Ce champ participe à `D("scale/open", ...)` uniquement lorsqu'il existe : les octets/racines v0.5 sont préservés. Les identifiants d'implémentation sont `civic-go-v0.6` et `civic-typescript-v0.6`.

`civic-election-1` contient la preuve d'ouverture, tous les points de contrôle réellement acceptés et leurs **octets exacts** d'en-tête signé, les huit partitions y compris vides, chaque lot et chaque reçu natif original, les références de toutes les enveloppes, la preuve du résultat provisoire, la certification éventuelle et une preuve du statut à une hauteur indiquée. L'export conserve les éléments chiffrés publics nécessaires, sans répertoires de validateurs, index, secrets d'identité ou clés de gardiens. Le serveur permet de télécharger les éléments observés même lorsqu'un auditeur découvrirait une erreur.

Les deux auditeurs lisent les objets par empreinte et recalculent indépendamment l'admission Semaphore, les preuves ElectionGuard et de connaissance, le premier nullifier valide, les doublons, les journaux RFC 9162, les racines de statuts/nullifiers, tous les agrégats et le déchiffrement global. Un lot ou une partition manquant, même vide, bloque l'attestation. Un index de téléchargement ou une signature d'archive ne remplace jamais ce rejeu.

L'identifiant de dossier est `D("audit/dossier", [network_id,poll_id,epoch,context_id,points_de_controle_ordonnés,aggregate,count,yes,no,SHA256(tally_bundle)])`. L'attestation signe `D("audit/attest", [network_id,poll_id,epoch,dossier_id,implementation])`. Les clés et identifiants d'implémentation sont figés à l'ouverture. La signature rend l'auditeur responsable de sa déclaration ; elle n'atteste pas matériellement le processus exécuté. La fixture TypeScript est produite par le véritable exécutable TypeScript après rejeu, puis consommée par le générateur Go.

La certification nécessite **3/5 auditeurs distincts et les deux implémentations**, attend **30 minutes** depuis la publication et expire à **clôture + 48 heures**. Une certification donne accès aux règles ordinaires d'adoption et au délai d'effet ; un total provisoire n'applique aucune loi. Le rapport porte `as_of_height` : il ne prétend pas prouver l'absence d'une contestation future.

`audit.challenge` est borné à deux enveloppes par preuve et quatre commandes par bloc. Les preuves lient enveloppe admise, chemin RFC 9162 et statut sous le scellé accepté : bulletin invalide compté, mauvais routage, double comptage du nullifier, ou preuve cryptographiquement valide marquée `invalid`. L'absence d'un nullifier seule ne prouve pas qu'un statut `duplicate` soit erroné : ces omissions plus générales sont détectées par le rejeu intégral et bloquent les attestations. Une contestation valide est traitée **avant les effets automatiques du même bloc** et inscrit `contested`. Une preuve fausse ne modifie aucun état. Un effet déjà appliqué reste dans l'historique et exige une correction publique ultérieure.

## Reproduction et limites

Les fichiers `*.TEST_ONLY.json` portent `PUBLIC_TEST_ONLY`. Les rôles `archive` et `audit` dérivent de domaines de test distincts selon 0011. Les jeux de données utilisent de vraies preuves mais des identités publiques et des commits synthétiques liés pour l'archive figée.

Régénération explicite depuis la racine : `go run ./cmd/civnode fixtures`, puis `npm run build`, `node scripts/audit-citizen.mjs -input spec/fixtures/v0.6/record -manifest spec/fixtures/v0.6/network.json -attest-key spec/fixtures/v0.6/audit-2.TEST_ONLY.json -output spec/fixtures/v0.6/audit-ts.json`, puis `go run ./cmd/civnode fixtures -certified`. Le contrôle habituel est `go run ./cmd/civnode fixtures -check -certified` : il ne remplace pas la fixture certifiée par le dossier provisoire.

`scripts/check.ps1` exerce le rejeu différentiel, les refus, les quorums, les délais, l'arrêt brutal d'archives et les réseaux natifs. `scripts/test-citizen-native.mjs` lance quatorze validateurs dans des processus isolés, synchronise un observateur principal + partition, vérifie une preuve en TypeScript et tue/reprend l'observateur avec reconstruction de l'index. La démonstration `scripts/citizen.ps1` utilise deux archives HTTP dans des processus distincts.

Ces tests locaux ne qualifient pas la capacité à dix millions, les pannes WAN, la résistance à la corrélation réseau, le stockage matériel ou l'indépendance réelle des opérateurs. OCI, publication des binaires, déploiement, outillage d'urgence et PWA restent hors de ce jalon.
