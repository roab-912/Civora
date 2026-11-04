# ElectionGuard v0.4 — contrat disponible et mapping restant

Statut : **préparation seulement**. Cette annexe révise l’état d’intégration de la RFC conformément au §12.1 ; elle ne déclare ni la suite ni le jalon v0.4 qualifiés. La [décision 0009](../decisions/0009-electionguard-interop.md) donne les pièces et les motifs. Les exigences cryptographiques du §7 et du §14 restent obligatoires.

## Frontière Civic implémentée

| Élément | Octets et contrôle |
|---|---|
| `ceremony_bundle`, `encrypted_ballot`, `decryption_record` | Base64url strict d’une enveloppe JCS Civic `{files,suite}`. |
| `suite` | Chaîne exacte `eg-2.0-civic-1`. Cette valeur dans un fichier ne vaut pas autorisation de registre. |
| `files[]` | Objets exacts `{bytes,path}`, triés selon les chemins ASCII, sans entrée répétée ou supplémentaire. |
| `bytes` | Base64url strict d’octets opaques, préservés sans réencodage JSON. |
| Cérémonie | `context.json`, `guardians/01.json` à `guardians/05.json`, `link.json`, `manifest.json`. |
| Bulletin | Seulement `ballot.json`. |
| Total | Seulement `tally.json`. |
| `link.json` | Objet JCS strict `{network_id,poll_id,proposal_id,question_digest}`, identifiants hexadécimaux minuscules de 32 octets ; `question_digest = D("poll/question",[network_id,poll_id,proposal_id])`. |
| Ressources locales | 128 Kio par enveloppe décodée, 64 Kio par fichier, 90 Kio de fichiers décodés au total. Les limites du bloc et de la transaction restent indépendantes. |
| Inspection d’archive | SHA-256 de l’enveloppe et de chaque fichier, tailles décimales en chaînes ; `cryptography_verified:false`, `suite_qualified:false`. |

Les décodeurs ne sont pas deux vérificateurs ElectionGuard. Ils n’interprètent pas les fichiers opaques : ils peuvent donc inspecter l’enveloppe d’un faux bulletin. Les schémas `eg_bundle` et `eg_link` ont la même portée limitée. Le schéma strict d’un bulletin chiffré valide reste à fournir.

## Mapping cryptographique bloquant

| Donnée Civic | Exigence conservée | Travail requis avant activation |
|---|---|---|
| `manifest.json` | Élection `poll_id`, concours `civic_question`, sélection obligatoire `yes_bit`, limite 1. | Mapping de tous les champs, sérialisation exacte et empreinte de manifeste conforme au design 2.0. |
| `context.json` | Groupe standard verrouillé, cinq gardiens, seuil trois, clé jointe fraîche, empreintes de contexte. | Mapping des paramètres, des empreintes et de la clé ; recomputation indépendante. |
| `guardians/NN.json` | Numérotation par `account_id`, preuves publiques de chaque gardien. | Mapping de la cérémonie distribuée, vérification des preuves, contrôle des parts échangées par les gardiens et des cinq attestations civiques. Aucune génération centralisée d’un secret commun ne peut remplacer cette cérémonie. |
| `ballot.json` | Chiffré ElGamal de 0 ou 1, toutes les preuves exigées, code de confirmation, identifiant frais. | Compléter les champs ouverts par la sérialisation 0.0.1, convertir et vérifier des vecteurs externes, rejeter sélection absente et champs révélateurs ; documenter les métadonnées communes. |
| Agrégat | Produit homomorphe des seuls bulletins acceptés ; doublons et challenges exclus. | Encodage du chiffré, vecteurs d’agrégation et liaison à la clôture et aux exports. |
| `tally.json` | Déchiffrement vérifiable du seul agrégat global ; aucune ouverture individuelle normale. | Définir la sérialisation absente du document 0.0.1, mapper le protocole de preuve 2.0, vérifier seuil et résultat avec deux implémentations. |
| Indépendance `scale_1` | RFC 8235 sur `A=g^r`, sous-groupe et `A!=1`, contexte lié au nullifier et au chiffré. | Qualifier l’export éphémère de `r` depuis le chiffreur existant et figer le transcript, les longueurs et les encodages. |
| Liaison Semaphore et partition | `scale/scope`, `scale/message`, nullifier du SDK, routage `scale/lane`. | Vecteurs croisés d’enveloppes réellement chiffrées, refus d’une copie réétiquetée et d’une partition étrangère ; ne jamais réutiliser le binding `vote/*` d’`inline_dev` comme substitut. |

La spécification 0.0.1 seule ne détermine pas ces octets. Il faut compléter cette table par le mapping **champ par champ** et un corpus externe vérifié avant de prétendre à l’interopérabilité. La voie proposée conserve les algorithmes 2.0 et définit explicitement les compléments Civic nécessaires, avec un adaptateur et ses conversions verrouillés. Aucun changement automatique vers le design 2.1 n’est effectué.

Les points de contrôle authentifiés par le consensus et les reçus BFT relèvent de v0.5 ; v0.4 doit néanmoins qualifier les contrôles purs de contexte, de fraîcheur et de routage sur fixtures authentifiées. Ils ne sont pas exercés par les simples paquets présents ici. Le [rapport de qualification](../reports/v0.4-interop.json) les maintient parmi les critères non exécutés.
