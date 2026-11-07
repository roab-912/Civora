# Exécuter et documenter la qualification v0.9

Statut : prérequis locaux disponibles, qualification publique non obtenue. Les critères normatifs sont les §§14.5 et 15 de la [spécification](../Protocole-Civique-v1.md). La [décision 0015](../decisions/0015-qualification-v0.9.md) décrit les formats, la confiance et les travaux restants.

## Contrôles locaux reproductibles

```powershell
npm run test:qualification
# Durée de fuzzing par cible, en secondes :
npm run test:qualification -- --fuzz-seconds 60
# Contrôle global, incluant les prérequis v0.9 :
./scripts/check.ps1 -Qualification
```

Ces commandes créent des dossiers isolés `.tools/qualification/v0.9-*`, sans toucher aux serveurs de l’atelier. Elles exécutent les tests de quorums, rejeu, reprise après arrêt brutal, rotation à double configuration avec approbation TEST_ONLY, contrôle de reçus réels et quatre campagnes de fuzzing. L’exercice CLI utilise la racine et les graines **PUBLIC_TEST_ONLY** de `spec/fixtures/v0.9/`. Aucun de ces résultats ne vaut qualification de capacité.

Elles couvrent également les suspensions prioritaires dans les machines, l’effet d’un vrai scrutin chiffré à son échéance, les dépôts d’une partition, les refus Go/TypeScript et les arrêts `SIGKILL` autour du commit. Un exercice distinct à sept processus CometBFT utilise une nouvelle configuration de laboratoire : `npm run build`, puis `node scripts/test-recovery-native.mjs`. La [décision 0016](../decisions/0016-suspensions-locales-v0.9.md) fixe cette extension et ses limites. La PWA actuelle conserve une consultation seule pour ces configurations.

## Installation à qualifier

Avant une campagne réelle, documenter les 32 partitions, leurs sept sièges, les organisations, domaines d’hébergement, d’administration et de conservation des clés, les archives, les deux voies d’accès indépendantes et les cinq auditeurs. Publier matériel, versions, limites de files, CPU réservés, réseau, stockage, sauvegardes, coûts et procédure de restauration. Séparer les ressources d’admission et de contrôle complet. Préparer dix millions de bulletins frais, puis le cas des secondes tentatives jusqu’à vingt millions d’admissions. Les copies d’un même bulletin ne sont pas des votes supplémentaires.

Conserver les flux bruts, commandes, horloges de mesure, erreurs et résultats de rejeu. Étiqueter clairement émulation WAN, réseau réel et données de développement. Documenter aussi les tailles moyenne, p95 et maximale des enveloppes, les volumes par opérateur et la génération sur téléphones physiques. Tester l’afflux de lectures après Push et publier la méthode de mesure.

## Mesurer les reçus

```powershell
go build -o .tools/civqual.exe ./cmd/civqual
.tools/civqual.exe -context frozen.json -context-pin SHA3_384_AUTHENTIFIE -samples receipts.jsonl -start-ms DEBUT -end-ms FIN -output receipts-report.json
```

`frozen.json` est un contexte gelé canonique déjà authentifié. Le pin vient d’une source indépendante du fichier de mesures. Chaque ligne de `receipts.jsonl` est canonique, avec exactement les trois champs suivants, triés :

```json
{"receipt":null,"received_at_ms":"2000","sent_at_ms":"1000"}
```

Remplacer `null` par un reçu natif complet pour un dépôt ; conserver `null` pour un échec de transport. Relever le début juste avant transmission des octets prêts et la fin à l’obtention du reçu, dans une même horloge côté client. Le collecteur vérifie chaque preuve, compte les échecs, exclut les doublons du débit et conserve les secondes vides. Un reçu répliqué ne prouve pas un bulletin complètement valide : conserver séparément les traces de contrôle complet et les rejeux Go/TypeScript.

Créer des rapports distincts pour le nominal à dix millions, la pointe de quinze minutes à 5 000 reçus/s et le pic ciblé d’une partition à 320/s. L’ensemble de déduplication nécessite de la mémoire proportionnelle au nombre d’enveloppes distinctes. Exécuter le collecteur sur une machine de vérification dimensionnée et publier ce coût séparément des mesures du réseau.

L’option `-output` écrit directement un nouveau fichier UTF-8 canonique et refuse d’écraser une pièce existante. Elle évite les conversions de texte des redirections PowerShell. Les fixtures `frozen.TEST_ONLY.json` et `receipts.TEST_ONLY.jsonl` fournissent un exemple reproductible : un reçu unique, un doublon, une preuve invalide et un échec de transport, avec commits synthétiques de développement.

## Vérifier une campagne

Copier `campaign.example.json` dans le dossier de campagne et remplir ses références avec les pièces effectivement produites. Son état initial reste non qualifié. Les rapports de vérification doivent inclure le cas du retard complet, vingt millions de contrôles, les statuts terminaux, le dépouillement, les trois attestations d’audit et la fenêtre de contestation. Faire effectuer les audits cryptographique et applicatif par des organisations indépendantes et corriger les défauts critiques et élevés. Publier les remédiations et les risques résiduels.

La campagne, la politique et les rapports de reçus utilisent le JSON canonique du protocole, avec au plus un LF final de cadrage. Les doublons de propriétés et les variantes d’encodage sont refusés. `source_revision` est le SHA-256 des octets exacts de l’archive de sources auditée, distinct d’un identifiant Git court.

Installer la politique des cinq contrôleurs indépendants par un autre canal et en obtenir l’empreinte SHA-256 indépendamment de la campagne. Trois contrôleurs signent le contenu canonique sans `attestations`, avec le domaine décrit dans la décision 0015.

```powershell
npm run qualify -- --campaign campagne/campaign.json --policy controleurs.json --policy-pin SHA256_AUTHENTIFIE --output campagne/report.json
```

Le code de sortie `2` indique des critères non satisfaits. La suite de vote actuelle reste verrouillée comme non qualifiée publiquement. L’outil authentifie les pièces et déclarations et applique les seuils ; l’audit indépendant doit contrôler la véracité des mesures et l’indépendance des organisations.

## Exercice de reprise sans consensus

```powershell
go build -o .tools/civrecovery.exe ./cmd/civrecovery
.tools/civrecovery.exe verify -root racine.json -root-pin SHA3_384_AUTHENTIFIE -bundle ordre.json
.tools/civrecovery.exe apply -root racine.json -root-pin SHA3_384_AUTHENTIFIE -bundle ordre.json -store .dev-host/recovery/counters.db
.tools/civrecovery.exe status -root racine.json -root-pin SHA3_384_AUTHENTIFIE -store .dev-host/recovery/counters.db
.tools/civrecovery.exe hash-archive -archive archive-canonique.bin
```

La racine doit avoir été installée indépendamment ; un ordre ne peut la choisir. Prévoir les cinq signatures de chaque collège et trois observations de miroirs récents. Le même ordre est refusé après redémarrage. Une observation périmée laisse le composant en lecture seule. Les commandes ne modifient ni registre ni résultat et ne lancent aucun import. L’option `-archive` d’un `recovery.publish` compare la référence signée aux octets d’archive eux-mêmes.

En exercice, mesurer confirmation de l’incident, obtention des signatures, réception par les composants et confinement effectif ; la cible de deux heures ne se déduit pas du temps de vérification cryptographique. Exercer la clôture, la perte de quorum, le cas sans remplacement sûr, la rotation, la compromission de distribution et la récupération des reçus. Configurer avant le pilote les responsables, le canal confidentiel, l’astreinte, les canaux publics et le prochain point de situation. Ces éléments d’exploitation, la distribution signée, les autorisations constitutionnelles et le cycle complet de reprise restent bloquants.
