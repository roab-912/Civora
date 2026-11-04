# 0004 — Journal atomique de développement

Statut : adopté pour v0.1. Références : §§4.2, 12.3.

Le port de stockage est séparé du moteur. L’adaptateur local conserve une genèse canonique exacte et un fichier immuable `00000000000000000001.commit.json` par hauteur. Un enregistrement contient le bloc, l’état intégral après bloc, les événements et `app_hash`. Le nom, la hauteur, la continuité et le contenu sont contrôlés lors du rejeu. Le marqueur de reprise est la présence de cet enregistrement complet, pas un compteur écrit séparément.

Un commit écrit un fichier temporaire dans le même répertoire, appelle `File.Sync`, ferme le fichier, le renomme vers le nom de hauteur définitif puis synchronise le répertoire lorsque le système le permet. L’état visible en mémoire est remplacé après ces opérations. Les temporaires sont ignorés à la reprise ; une écriture partielle avec nom définitif est une corruption et provoque un arrêt. Après une erreur d’entrée/sortie, l’appelant doit rouvrir le stockage avant de poursuivre, car le renommage peut avoir abouti.

Au démarrage, les blocs sont rejoués depuis la genèse, sans charger aveuglément l’état matérialisé. L’état, les événements et la racine recalculés doivent être identiques au fichier. Une genèse différente est refusée. Les identifiants de transactions finalisées sont reconstruits pour retrouver le même statut après retransmission. La CLI compare aussi les blocs déjà durables au scénario demandé ; une fixture portant un autre historique est refusée.

Le stockage est destiné à **un seul écrivain par répertoire**. Un mutex sérialise les appels d’une instance ; plusieurs processus écrivains sur le même répertoire ne sont pas pris en charge. Les tests tuent réellement le processus après une demi-écriture, après écriture, après synchronisation, après renommage et après synchronisation du répertoire. Ils couvrent une rotation et le bloc d’activation, puis comparent tous les enregistrements à une exécution ininterrompue.

Sous Windows, Go n’expose pas le `fsync` de répertoire par `os.File`. Le contrat testé est la mort du processus ; la résistance à une coupure électrique, les écritures multi-processus, le stockage incrémental et la réplication BFT exigent un adaptateur qualifié ultérieur. Un export local auto-cohérent ne constitue pas une preuve de finalité BFT ou d’authenticité publique. Le transport et les signatures de consensus seront raccordés en v0.5.
