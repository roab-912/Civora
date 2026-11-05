# 0011 — Consensus natif et transport partitionné de développement

Date : 2026-11-05. Portée : v0.5 de développement. Cette décision complète [0010](0010-electionguard-v0.4.md), sans qualifier un pilote public ni démarrer v0.6.

## Dépendance et genèse

CometBFT **v0.38.26**, commit `94d77f9f51a72e2b7d832798859f6222f08028f8`, remplace la réservation v0.38.17 du verrou initial. Les sommes sont celles de `go.sum` : module `h1:MIPYgOSvyyiN1jo3SZljKy4ZXqs896aQnj3pQFfUmaE=`, go.mod `h1:mDrAs+NMp7gc2ExoTE2/S6lDMespxaEc2Nd2BvDbbmM=`. Le [tag](https://github.com/cometbft/cometbft/tree/v0.38.26) fixe les messages, l'encodage des signatures et le décalage des mises à jour de validateurs.

`civnet` embarque le moteur, avec une application ABCI++ locale. La genèse native contient sept validateurs Ed25519 de poids unitaire, des blocs de 2 Mio au maximum et les extensions de vote désactivées. `app_state` est l'enveloppe stricte `network_genesis` : `kind` vaut `ledger`, `partition` ou `coordinator`, `data` contient sa configuration typée. Le champ optionnel `consensus_rotations` contient exclusivement des plans de genèse de développement. Les nombres natifs de CometBFT restent natifs ; les entiers Civic restent des chaînes décimales.

Le lanceur initialise seulement des adresses de boucle locale. Le fichier `development.json` fixe l'empreinte SHA-256 des **octets exacts** de la genèse native, le siège et les pairs. Le journal conserve aussi ces octets et refuse une divergence à la reprise. L'identifiant natif doit être celui de l'application. La confiance initiale exige une genèse authentique ; modifier ensemble les fichiers locaux ne constitue pas une preuve d'autorisation.

Les identités de développement sont publiques : `GenPrivKeyFromSecret(SHA256(UTF8("CIVORA/v0.5/TEST_ONLY/" + chain + "/" + role + "/" + seat)))`. Les rôles `consensus`, `p2p`, `archive` et `consensus-next` sont séparés. `GenPrivKeyFromSecret` applique son propre SHA-256 avant Ed25519. Les fichiers natifs de clés sont accompagnés de `README.TEST_ONLY.txt` et de l'étiquette `PUBLIC_TEST_KEYS_ONLY`. Une nouvelle initialisation refuse tout répertoire existant. Ces identités servent à reproduire les tests, jamais à établir une autorité de publication.

Le transport natif, ses paramètres et son algorithme de signature sont fixés par cette genèse et ce binaire ; ils ne sont pas sélectionnés à partir des anciens marqueurs `dev.unimplemented.*` du registre v0.1. Le registre applicatif continue de gouverner ses suites de transaction et ses epochs. Un changement de protocole natif exige une qualification distincte.

## ABCI++, persistance et reprises

`PrepareProposal` sélectionne un préfixe valide dans l'ordre proposé, en rejouant depuis l'état engagé pour conserver les règles H−1. `ProcessProposal` rejoue la proposition entière. `FinalizeBlock` garde un candidat en mémoire ; `Info` et `Query` exposent seulement l'état durable. `Commit` écrit un enregistrement temporaire, le synchronise puis le renomme. Le journal vérifie état et racine par rejeu à chaque ouverture. Le stockage historique v0.1–v0.4 reste compatible.

L'identité d'un bloc décidé comprend son hash natif et son horodatage RFC3339Nano exact. La répétition de `FinalizeBlock`/`Commit` sur le dernier bloc durable restitue les mêmes résultats et mises à jour de validateurs. Deux en-têtes différents dans la même milliseconde sont distincts. Après une erreur de persistance, l'application exige un redémarrage. Sous Windows, ces tests qualifient la mort du processus, pas une coupure électrique.

Le verrou exclusif de chaque répertoire est acquis avant l'ouverture du signataire et des bases. La reprise exige la clé **et** `priv_validator_state.json` existants. Elle n'utilise ni génération automatique, ni état de signature vide. Le signataire natif conserve la signature d'un même vote et refuse un vote contradictoire à hauteur/tour/type identiques. Les blocs vides restent actifs. Le temps applicatif vaut `max(précédent, floor(header.time/ms))`.

L'import d'instantané est refusé ; le rejeu depuis la genèse est le chemin disponible. Les requêtes ABCI `/state` et `/proof` servent l'état engagé courant. Les archives sélectives, les chemins historiques et les relais publics relèvent de v0.6.

## Preuves et quorums

Une preuve d'état contient `height`, `signed_header` et un chemin clairsemé de **256 frères de la racine vers la feuille**. Le chemin conserve le hachage du §4. Le vérificateur exige l'en-tête **H+1** pour une valeur après H. Les chaînes et clés proviennent de la genèse ou du contexte figé authentifié, jamais de la preuve elle-même. Sept clés distinctes de poids unitaire sont requises et cinq signatures de commit sont nécessaires. Les en-têtes sont bornés à 48 Kio après décodage.

`signed_header` transporte, en Base64url strict, le JSON natif de `types.SignedHeader`. Ses champs sont authentifiés par l'encodage protobuf natif et les signatures ; ce JSON externe n'est pas réécrit en JCS Civic. Go emploie le vérificateur CometBFT épinglé. TypeScript reconstruit indépendamment le hash de l'en-tête, le hash des validateurs, le `CanonicalVote` préfixé de sa longueur et les signatures Ed25519. Ce vérificateur utilise un jeu déjà authentifié ; il ne remplace pas un client léger avec période de confiance et découverte de transitions.

Les commits natifs utilisent **ZIP-215**, y compris ses encodages de points non réduits. Le vérificateur TypeScript emploie `@noble/curves` **2.4.0**, installé sous l'alias `@noble/curves-comet` pour conserver la dépendance transitive Semaphore inchangée, avec `zip215:true` explicite. Le [code épinglé](https://github.com/paulmillr/noble-curves/blob/2.4.0/src/abstract/edwards.ts) lie le challenge aux octets reçus de R et A. `zip215.json` contient un commit à cinq signatures dont un R non réduit : Go et TypeScript l'acceptent ; remplacer R par son encodage canonique sans recalculer la signature échoue. Les signatures Civic et d'archive gardent leur vérification Ed25519 habituelle, distincte de cette règle native.

Les reçus de dépôt utilisent la preuve native d'une transaction dans le **bloc H**, son en-tête signé H, le chemin de l'enveloppe dans le lot et le certificat d'archive. Ils établissent un dépôt durable, même si la vérification différée rejette ensuite ce bulletin. Ils ne prouvent pas un vote compté. La date du bloc doit être dans `[ouverture, clôture[`.

## Profil et ouverture

`transport_dev_v0_5` est un profil explicite de développement, distinct de `scale_1`. Son noyau utilise les vrais adaptateurs ElectionGuard et Semaphore. La genèse de fixture active la profondeur 26 et conserve le module 16 en vérification historique. Le groupe d'ouverture reste le groupe trié H−1 du pilote v0.4 ; le remplacement par le parcours institutionnel de masse n'est pas présenté comme qualifié.

Le coordinateur projette automatiquement un scrutin réellement ouvert dans `transport_polls`. Cette entrée fige réseau, scrutin, epoch, racine admissible, profondeur, nombre de partitions, identifiant de proposition, cérémonie, gardiens, dates, hauteur d'ouverture, effectif, jeux de validateurs des partitions et cinq clés d'archive. Le contexte porte `checkpoint_id = D("scale/open", [network,poll,epoch,root,depth,P,proposal,guardians,ceremony,opens,closes,source_height,lanes,archive_keys,eligible_count])`.

Chaque partition importe cette entrée par preuve d'état du registre principal. Sa chaîne est `chain_id_principal + "-v" + hex_minuscule_sur_deux_chiffres(lane)`. Le scrutin prend les P premières partitions du manifeste, P puissance de deux de 1 à 256. La fixture chiffrée en utilise huit, avec des partitions vides. La cérémonie de développement reste trois gardiens sur cinq ; elle ne revendique pas le profil public proposé de sept sur onze.

## Admission, traitement et archives

Une transaction `partition.admit` transporte les octets exacts des enveloppes et un certificat. L'admission contrôle schéma, bornes, domaine figé, routage, calendrier et vraie preuve Semaphore liée aux empreintes du bulletin et de la preuve d'indépendance. Les preuves ElectionGuard et RFC 8235 sont vérifiées ensuite, dans l'ordre d'admission, par `partition.process`. Son compteur est une demande de travail, jamais un verdict de worker accepté sur parole.

Les budgets sont distincts : au plus **32 nouveaux dépôts**, **4 vérifications complètes** et 2 Mio par bloc ; 256 Kio par transaction. Au plus deux enveloppes distinctes par nullifier sont admises. Un renvoi d'octets connus restitue les positions existantes sans consommer de place, y compris après clôture. Un lot mêlant anciens et nouveaux dépôts doit séparer ses nouveaux octets pour que les bornes signées restent sans ambiguïté. Le certificat et la hauteur d'origine ne sont pas remplacés par ceux du réessai.

Le premier bulletin complètement valide consomme le nullifier et modifie l'agrégat atomiquement. Un dépôt invalide garde un statut terminal `invalid` et ne consomme rien. Les statuts suivants peuvent être `duplicate` ou `counted`. Le rejet d'un bloc laisse le journal, les statuts et l'agrégat précédents intacts.

Pour un lot, `objects` est la liste ordonnée `{sha256: SHA256(envelope_bytes), size: decimal_byte_length}`. `manifest_sha256 = SHA256(JCS(objects))`. Sa racine RFC 9162 utilise les octets exacts des enveloppes. La référence contient réseau, scrutin, partition, epoch, `start`, `count`, racine, empreinte du manifeste et `retention:"persisted-development-v0.5"`.

L'identité du lot est `D("scale/batch", [network,poll,lane,epoch,start,count,root,manifest,retention])`. Chaque signature d'archive porte les 32 octets de `D("scale/archive", [network,poll,lane,epoch,start,count,root,manifest,retention])`. Trois signatures distinctes sur cinq, triées par clé hexadécimale, sont requises. La disponibilité reste un engagement des signataires ; le service de miroir et le téléchargement indépendant sont à réaliser en v0.6.

Le journal d'admission contient les enveloppes ; le journal de lots contient `JCS(BatchRef)`, identité comprise. Les deux utilisent RFC 9162 : feuille `SHA256(0x00 || bytes)`, parent `SHA256(0x01 || left || right)`, vide `SHA256("")`. Les chemins d'inclusion vont de la feuille à la racine. Les preuves de cohérence suivent l'algorithme RFC pour les préfixes, sans dupliquer la dernière feuille.

## Points de contrôle, scellés et résultat

`lane_checkpoint[poll_id]` expose compteurs, racines d'admission, de lots, de statuts et de nullifiers, agrégat et deux scellés. La clôture scelle l'admission ; le scellé de traitement exige un statut terminal par dépôt et `processed_count == admitted_count`. Les racines des ensembles vides sont contrôlées explicitement.

Chaque bloc d'une partition active émet `lane.checkpoint.ready`, ainsi que le premier bloc qui termine son scellé. Cela rend un point disponible à chaque bloc, y compris vide ; le relais peut collecter le chemin puis l'en-tête H+1. Les événements métier du noyau sont également restitués par ABCI et reconstruits au rejeu. Le service de relais et sa surveillance de cadence restent en v0.6.

Le coordinateur exige la preuve native H+1, le bon contexte figé, une hauteur croissante, le lien avec l'identité du dernier point enregistré, les deux preuves de cohérence et chaque nouveau lot avec inclusion et certificat. L'identité enregistrée vaut `D("scale/checkpoint", [height,signed_header,summary])`. Le premier prédécesseur est 32 octets nuls. Une extension comporte au plus 64 lots et un bloc principal au plus 32 commandes de transport, indépendamment du trafic de comptes.

Le profil de développement exécute les échéances et les transactions civiques du noyau, puis les commandes de transport du bloc dans leur ordre relatif. La projection d'ouverture utilise toujours H−1. Cette séparation de phases est un choix d'interopérabilité propre à `transport_dev_v0_5` ; elle ne définit pas l'ordonnancement final du profil public `scale_1`, notamment celui des contestations avant effets.

Il faut un point final pour **chaque** partition, même vide. Le coordinateur combine les chiffrés dans l'ordre numérique des partitions et additionne leurs compteurs. La multiplication contrôle l'appartenance au groupe verrouillé et autorise son élément neutre pour une partition vide. Aucun total partiel ne peut être déchiffré par la voie normale. La preuve de total v0.4 est vérifiée sur le seul agrégat global.

Le scrutin passe de `open` à `processing`, puis à `provisional` après vérification de tous les scellés et du total. Le résultat reste séparé des champs de certification et n'autorise aucun effet. Un scrutin vide exige tous ses scellés et aucun document de déchiffrement. Sans certification à 48 h de la clôture, il passe à `failed_certification`. Les attestations, contestations et sources d'audit indépendantes restent à v0.6. La preuve d'un quorum ne constitue pas à elle seule cet audit.

## Rotations et versions

Les plans de rotation portent `authorization:"development_genesis_fixture"` ; ils sont engagés dans la genèse, pas autorisés par un vote des validateurs. Une mise à jour renvoyée par `FinalizeBlock(H)` est effective à **H+2**. Les barrières H, H+1 et H+2 refusent un scrutin actif ou une nouvelle ouverture. Le calendrier exige un état déjà drainé ; il ne reporte jamais un reçu pour atteindre une hauteur.

L'initialisation prépare les identités de test futures, avec leur propre couple `priv_validator_key.<public_key>.json` / `priv_validator_state.<public_key>.json`. Le rôle de la première génération nouvelle est `consensus-next`, puis `consensus-next-N`. `serve -key-generation N` sélectionne un couple déjà présent et préautorisé. Une clé réutilisée retrouve toujours son ancien fichier d'état ; la sélection d'une génération ne crée ni ne réinitialise de fichier. Le test natif arrête deux autres validateurs et exige la signature de la nouvelle identité pour retrouver le quorum.

La rotation des gardiens vérifie cinq comptes civiques actifs et leurs clés courantes, incrémente l'epoch des gardiens et laisse intacts les contextes historiques. Les gardiens et validateurs n'utilisent pas les mêmes clés. Un plan applicatif d'epoch garde la barrière fixe et le refus avant signature des versions incompatibles ; les plans v0.1 de développement ne sont pas présentés comme un protocole public d'approbation ou d'armement multichaîne.

Les manifestes de partitions et leur confiance restent figés dans ce pilote. La distribution institutionnelle de nouvelles clés, les preuves d'armement coordonnées entre chaînes et la découverte par client léger restent à qualifier avant un usage public (§15). Aucun outil d'urgence, réinitialisation de signataire, déploiement ou publication n'est ajouté.

Les preuves probabilistes figées ne sont pas régénérées en vérification courante. Les fixtures de transport portent explicitement `SYNTHETIC_SIGNED_COMMITS` : leurs signatures sont réelles mais leurs en-têtes sont fabriqués pour avancer les calendriers. Les tests natifs lancent séparément sept processus, vérifient les commits et les reprises, puis les transitions de clés et de version. Ni ces essais de boucle locale, ni le corpus cryptographique ne sont un benchmark WAN ou à dix millions de bulletins.
