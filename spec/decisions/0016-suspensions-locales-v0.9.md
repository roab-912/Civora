# 0016 — Suspensions déterministes en laboratoire v0.9

Date : 2026-11-07. Statut : profil de développement explicitement activé dans la genèse, sans qualification publique. Complète la [décision 0015](0015-qualification-v0.9.md) après le choix de préparer et tester localement, sans infrastructure WAN ni rapports d’audit externes.

## Configuration et portée

Le champ optionnel `recovery` de la configuration du noyau et de celle d’une partition contient une racine complète, son `root_sha3_384` et `protected_profile`. Il est inscrit dans la genèse de laboratoire. L’empreinte porte sur les octets canoniques de la racine ; le profil protégé doit correspondre à celui de l’application. Cette première intégration accepte uniquement `development_TEST_ONLY`. Elle n’installe aucune racine dans une session existante. Une racine et son empreinte inscrites dans une genèse ne remplacent pas leur installation par des canaux indépendants pour le secours réel.

Le contexte gelé d’un scrutin partitionné reprend cette configuration. Son identifiant conserve la liste historique d’éléments, puis ajoute `recovery` après la configuration d’audit lorsqu’il existe. Go et TypeScript appliquent le même ordre. Une partition refuse une ouverture dont la configuration de reprise diffère de sa genèse. Sans ce champ, les octets et racines des fixtures historiques restent identiques.

Le fil natif ajoute l’enveloppe stricte `security_hold_tx` : `{"type":"security.hold","value":bundle}`. Le `type` signé de l’ordre doit concorder. Le bloc de développement du noyau transporte les bundles dans le champ optionnel `security_orders`. Les autres types d’ordre ne peuvent pas emprunter cette voie pour activer une réserve ou importer un état.

## Vérification et priorité

Les deux quorums ML-DSA-65 5/7, le réseau, l’epoch de reprise, le compteur et la durée maximale de 72 h sont vérifiés avec la configuration authentifiée et **le temps du bloc**. Les bundles sur chaîne portent une liste `mirrors` vide. Les observations des miroirs et l’horloge d’exploitation restent propres à la vérification hors chaîne ; elles ne deviennent pas un oracle de temps pour les transitions.

Au plus quatre ordres sont traités dans un bloc. Les tailles globales et individuelles restent bornées. `PrepareProposal` privilégie les enveloppes de suspension, même si des transactions ordinaires arrivent avant elles dans le mempool. La machine traite tous les ordres avant les transitions d’échéance, les admissions, les contrôles et les effets concernés du même bloc, indépendamment de leur place dans la proposition reçue. Un bloc comportant ensuite une opération suspendue est rejeté atomiquement. Le candidat et ses compteurs ne deviennent visibles qu’après `Commit`.

Les collèges n’obtiennent aucun pouvoir de vote, d’effet de loi ou de retour à une primitive révoquée. Une demande de suspension des engagements d’état ou du consensus est refusée sur cette voie : la procédure hors chaîne et une reprise indépendante restent nécessaires lorsque ces bases ne sont plus fiables.

## État, interruptions et consultation

Le compteur authentifié est conservé dans l’entrée `security/recovery`. Chaque bundle signé figure séparément sous `security_orders/{counter}`, pour conserver des preuves de taille bornée et la justification exacte de l’incident. Les journaux existants rejouent ces transitions pour reconstruire les racines et vérifier les redémarrages.

Les portées `poll`, `effect` et `profile` ciblent respectivement un identifiant de scrutin, une proposition portant la décision et le profil de l’application. Une proposition concernée avant application devient `interrupted`, ainsi que son scrutin. Les résultats, agrégats, dates prévues, dépôts et preuves restent conservés. Un effet déjà appliqué n’est pas réécrit. L’expiration de la pause ne reprend jamais automatiquement un scrutin interrompu. Une nouvelle décision sûre reste nécessaire.

La portée `module` interdit les nouvelles autorisations de ce module. Pour les primitives d’éligibilité, de bulletin ou de dépouillement, elle interrompt aussi les propositions et scrutins concernés. Le profil de laboratoire conserve un état de revue de sécurité au-delà de l’expiration de la pause : une primitive dont la confiance a été retirée ne redevient pas utilisable à l’écoulement du temps. Il n’existe aucun renouvellement automatique du hold ni aucune commande d’urgence pour retirer cette revue. L’utilisation d’une nouvelle identité de module exige les autorisations ordinaires sûres ; la réautorisation constitutionnelle et les réserves restent à intégrer et qualifier.

Dans une partition, les nouveaux dépôts et le traitement des dépôts concernés sont refusés. Un renvoi exact d’un lot déjà déposé retrouve son ancien dépôt et son certificat. Le point de contrôle porte `interrupted: true` ; cette propriété est absente dans le format historique. Il ne peut pas devenir un scellé de traitement certifiable. Les vérificateurs Go et TypeScript le refusent, même avec un authentique quorum de signatures natives. Les reçus de dépôt historiques continuent de se vérifier.

La PWA actuelle reconnaît la présence d’une configuration de reprise dans son état authentifié et se limite à la consultation pour ce profil. Elle n’envoie aucune transaction sans savoir opérer ces règles. Un futur client capable de vérifier indépendamment le secours dans le navigateur pourra qualifier des restrictions plus précises ; ce comportement n’est pas annoncé ici.

## Exercices reproductibles et limites

Les nouvelles fixtures `onchain-hold.TEST_ONLY.json` et `interrupted-checkpoint.TEST_ONLY.json` sont déterministes et contrôlées par les tests. La première compare racines et événements Go/TypeScript, y compris après expiration ; la seconde refuse indépendamment un point de contrôle interrompu signé par les clés natives publiques de développement.

Les exercices du noyau utilisent le scrutin chiffré réel v0.4 jusqu’à son adoption, puis suspendent son effet dans le bloc exact de l’échéance. Ceux de partition utilisent de vraies preuves de bulletin et d’éligibilité, des certificats recalculés et des commits synthétiques explicitement TEST_ONLY. Ils conservent le reçu et refusent reprise et certification. Les tests ABCI contrôlent la priorité, l’absence d’exposition spéculative, le redémarrage et un `SIGKILL` avant et après le renommage durable du commit.

`scripts/test-recovery-native.mjs` lance séparément sept vrais processus CometBFT sur loopback, avec une configuration de test et des clés publiques de développement. Il vérifie finalisation de l’ordre à double quorum, refus des opérations concernées, accord des sept répliques, signatures natives et conservation de la suspension après arrêt brutal d’un validateur. Les clés des aides `internal/recoverytest` ne font pas partie des dépendances des binaires de nœud.

Ces exercices ne qualifient ni le WAN, ni l’indépendance des opérateurs, ni le confinement opérationnel sous deux heures. La propagation d’un ordre entre tous les composants n’est pas garantie par une vérification locale : chaque composant doit authentifier l’ordre avant de pouvoir l’appliquer. Les racines réelles, le raccordement constitutionnel, la distribution signée, la reprise avec nouveau réseau, les réserves, les audits et les mesures de masse restent bloquants pour la qualification publique.
