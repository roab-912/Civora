import * as schemas from "civic-validators";
import { Buffer } from "./buffer.js";
import { Identity } from "@semaphore-protocol/identity";
import {
  installValidators,
  canonical,
  parse,
  domain,
  sha,
  validate,
} from "./protocol.js";
import {
  Client,
  inspectContext,
  verifyTransaction,
  type Row,
  type NetworkChoice,
} from "./network.js";
import {
  newAccount,
  exportBackup,
  importBackup,
  saveAccount,
  loadAccount,
  storedBackup,
  forgetAccount,
  credentials,
  encryptPending,
  pending,
  requireWebCrypto,
  type LocalAccount,
  type BackupSecrets,
  type Backup,
  type PublicAccount,
} from "./vault.js";
import { read, write } from "./storage.js";
import {
  settings as notificationSettings,
  pushCapability,
  enablePush,
  disablePush,
  updatePreferences,
  getNews,
} from "./notifications.js";
import { verifyInclusion, logRoot } from "../consensus.js";
import { checkProof, trustAt } from "./network.js";
import { verifyHeader } from "../consensus.js";
import { blob, uint } from "./protocol.js";
import {
  inspectExplorerBlock,
  explorerHeight,
  type ExplorerBlock,
} from "./explorer.js";
import {
  verifyArchiveCertificate,
  verifyWitness,
  type Frozen,
} from "../citizen-audit.js";
installValidators(schemas);
const root = document.querySelector<HTMLElement>("#app")!,
  notice = document.querySelector<HTMLElement>("#notice")!,
  connection = document.querySelector<HTMLElement>("#connection")!;
let client: Client,
  account: LocalAccount | null = null,
  choices: NetworkChoice[] = [],
  development = false,
  busy = false,
  routing = 0,
  generation = 0,
  activeWorker: Worker | null = null;
let notificationRoot: string | null = null;
let participationAvailable = false;
let networkPoll: ReturnType<typeof setInterval> | null = null;
let workerReject: ((error: Error) => void) | null = null;
function cancelWorker() {
  activeWorker?.terminate();
  activeWorker = null;
  workerReject?.(Error("ERR_OPERATION_CANCELLED"));
  workerReject = null;
}
const statuses: Record<string, string> = {
  collecting: "En recherche de soutiens",
  qualified: "Qualifiée",
  preparing: "Préparation du scrutin",
  scheduled: "Vote à venir",
  open: "Vote ouvert",
  tallying: "Dépouillement",
  processing: "Vérification en cours",
  provisional: "Résultat provisoire",
  adopted: "Adoptée · effet à venir",
  applied: "Appliquée au registre",
  rejected: "Rejetée",
  contested: "Contestée",
  failed_certification: "Certification échouée",
  active: "En vigueur",
  repealed: "Abrogée",
  retired: "Retiré",
  revoked: "Révoquée",
  expired: "Expirée",
  conflict: "Conflit de révision",
  failed_setup: "Préparation échouée",
};
const actions: Record<string, string> = {
  "law.create": "Créer une loi",
  "law.amend": "Amender une loi",
  "law.repeal": "Abroger une loi",
  "law.restore": "Rétablir une loi",
  "role.create": "Créer un rôle",
  "role.amend": "Modifier un rôle",
  "role.retire": "Retirer un rôle",
  "role.restore": "Rétablir un rôle",
  "role.grant": "Attribuer un rôle",
  "role.revoke": "Révoquer une attribution",
};
type Child = Node | string | null | undefined;
function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attrs: Record<string, string> = {},
  ...children: Child[]
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, v);
  for (const c of children)
    if (c !== null && c !== undefined)
      node.append(c instanceof Node ? c : document.createTextNode(c));
  return node;
}
const link = (text: string, path: string) =>
    el("a", { href: "#" + path }, text),
  pill = (state: string) =>
    el(
      "span",
      {
        class:
          "pill " + (state === "open" || state === "active" ? "positive" : ""),
      },
      statuses[state] ?? state,
    ),
  short = (s: string) => s.slice(0, 10) + "…" + s.slice(-6);
function date(s: string | null | undefined) {
  if (!s) return "Non fixée";
  const d = new Date(Number(s));
  return (
    new Intl.DateTimeFormat("fr-FR", {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(d) +
    " · " +
    d.toISOString().slice(0, 16).replace("T", " ") +
    " UTC"
  );
}
function flash(text: string, error = false) {
  notice.textContent = text;
  notice.className = error ? "notice error" : "notice";
  notice.hidden = false;
}
function errorMessage(e: unknown) {
  const message = e instanceof Error ? e.message : "ERR_UNAVAILABLE";
  if (message.includes("ERR_EXPLORER_HEIGHT"))
    return "Indiquez une hauteur entière positive disponible dans ce registre.";
  if (message.includes("ERR_EXPLORER_KEY"))
    return "L’identifiant public doit contenir 64 caractères hexadécimaux.";
  if (message.includes("ERR_ROLLBACK"))
    return "Connexion bloquée : ce registre annonce une hauteur inférieure à celle déjà vérifiée sur cet appareil (ERR_ROLLBACK). Vérifiez que le serveur utilise le bon registre. Vos données locales sont conservées.";
  if (message.includes("SECURE_CONTEXT"))
    return "La création de compte et le vote nécessitent HTTPS avec un certificat approuvé sur cet appareil. Ouvrez l’adresse HTTPS du serveur ; l’accès HTTP par adresse IP permet seulement la consultation.";
  if (message.includes("NETWORK") || message.includes("CONTEXT"))
    return "Ce contexte appartient à un autre réseau ou scrutin. Opération refusée.";
  if (message.includes("BACKUP_DECRYPT"))
    return "La sauvegarde ou le code de secours est incorrect.";
  if (message.includes("BACKUP_IDENTITY"))
    return "Ces clés ne correspondent pas au compte actuel.";
  if (
    message.includes("VERSION") ||
    message.includes("EPOCH") ||
    message.includes("SUITE")
  )
    return "La version ou la suite cryptographique de ce réseau n’est pas prise en charge. Le dépôt est bloqué.";
  if (message.includes("OFFLINE") || !navigator.onLine)
    return "Vous êtes hors ligne. Les données en cache restent consultables ; aucun vote ne sera envoyé automatiquement.";
  if (message.includes("PROOF") || message.includes("GENESIS"))
    return "La preuve ou la genèse ne peut pas être vérifiée. Opération refusée.";
  if (message.includes("PHASE"))
    return "Ce scrutin n’est plus ouvert. Aucun nouveau bulletin n’a été envoyé.";
  return (
    "Opération indisponible (" +
    message.slice(0, 100) +
    "). Vous pouvez réessayer."
  );
}
async function task(fn: () => Promise<void>) {
  if (busy) return;
  busy = true;
  document.body.setAttribute("aria-busy", "true");
  try {
    await fn();
  } catch (e) {
    flash(errorMessage(e), true);
  } finally {
    busy = false;
    document.body.removeAttribute("aria-busy");
    updateConnection();
  }
}
function button(text: string, fn: () => Promise<void>, kind = "primary") {
  const b = el("button", { type: "button", class: kind }, text);
  b.onclick = () => void task(fn);
  return b;
}
function updateConnection() {
  if (!client) return;
  const technical =
    location.hash.startsWith("#/network") || location.hash === "#/verify";
  connection.textContent =
    !navigator.onLine || client.offline
      ? "Hors ligne · données en cache"
      : client.versionOK
        ? technical
          ? "Source vérifiée · " +
            (client.stale ? "archive historique · " : "") +
            "hauteur " +
            client.height
          : "Connecté"
        : "Version non prise en charge";
  connection.dataset.state =
    client.offline || !navigator.onLine
      ? "offline"
      : client.versionOK
        ? "verified"
        : "blocked";
}
function header(kicker: string, title: string, description: string) {
  return el(
    "header",
    { class: "page-heading" },
    el("p", { class: "eyebrow" }, kicker),
    el("h1", { tabindex: "-1" }, title),
    el("p", { class: "lede" }, description),
  );
}
function setPage(...children: Child[]) {
  root.replaceChildren(
    ...(children.filter((c) => c !== null && c !== undefined) as (
      | Node
      | string
    )[]),
  );
  updateConnection();
  const heading = root.querySelector("h1");
  if (heading && document.activeElement === document.body)
    heading.focus({ preventScroll: true });
}
function renderPage(expected: number, ...children: Child[]) {
  if (expected !== generation) return;
  setPage(...children);
}
function cachedNote() {
  return el(
    "p",
    { class: "source-note" },
    client.offline
      ? "Vue en cache · dernière connexion : " +
          date(String(client.lastRead)) +
          ". Aucune action ne sera envoyée hors ligne."
      : "Expérimentation locale, sans effet juridique externe.",
  );
}
function title(p: Row) {
  return (
    p.changes?.title ??
    p.changes?.label ??
    actions[p.action] ??
    "Proposition civique"
  );
}
function card(p: Row) {
  const id = String(p.id);
  return el(
    "article",
    { class: "card proposal-card" },
    el(
      "div",
      { class: "card-top" },
      el(
        "span",
        { class: "eyebrow" },
        p.action?.startsWith("role") ? "Rôle civique" : "Texte de loi",
      ),
      pill(p.state),
    ),
    el("h3", {}, link(title(p), "/proposals/" + id)),
    el("p", { class: "muted clamp" }, p.rationale ?? ""),
    el(
      "div",
      { class: "card-footer" },
      el("span", {}, p.author ? "Dépôt public · " + short(p.author) : ""),
      link("Consulter →", "/proposals/" + id),
    ),
  );
}
async function list(resource: string, status = "", kind = "") {
  return (await client.get(
    "/v1/" +
      resource +
      "?limit=100" +
      (status ? "&status=" + encodeURIComponent(status) : "") +
      (kind ? "&type=" + encodeURIComponent(kind) : ""),
  )) as { height: string; items: Row[]; next_cursor: string };
}
async function home() {
  const viewGeneration = generation;

  const proposals = await list("proposals"),
    open = proposals.items.filter((p) => p.state === "open"),
    upcoming = proposals.items.filter((p) =>
      ["scheduled", "preparing"].includes(p.state),
    ),
    results = proposals.items.filter((p) =>
      ["adopted", "applied", "rejected", "contested"].includes(p.state),
    );
  renderPage(
    viewGeneration,
    header(
      "Votre espace civique",
      "Les décisions se construisent ensemble.",
      "Consultez les propositions, participez aux scrutins et suivez les textes en vigueur.",
    ),
    el(
      "section",
      { class: "stats", "aria-label": "Vue des décisions" },
      ...[
        [String(open.length), "Votes ouverts"],
        [String(upcoming.length), "À venir"],
        [String(results.length), "Décisions publiées"],
      ].map(([n, label]) =>
        el("div", {}, el("strong", {}, n!), el("span", {}, label!)),
      ),
    ),
    el(
      "div",
      { class: "section-title" },
      el("h2", {}, "À la une"),
      link("Toutes les propositions", "/proposals"),
    ),
    open.length
      ? el("div", { class: "cards" }, ...open.map(card))
      : el(
          "div",
          { class: "empty" },
          el("span", { class: "empty-mark", "aria-hidden": "true" }, "◌"),
          el("h2", {}, "Aucun vote ouvert pour le moment"),
          el(
            "p",
            {},
            "Une idée pour le collectif ? Déposez une proposition et rassemblez des soutiens.",
          ),
          link("Proposer un texte", "/new/law.create"),
        ),
    proposals.items.length
      ? el(
          "section",
          {},
          el("h2", {}, "Dernières propositions"),
          el(
            "div",
            { class: "cards" },
            ...proposals.items.slice(0, 6).map(card),
          ),
        )
      : null,
    el(
      "aside",
      { class: "principles" },
      el("h2", {}, "Un registre public, un choix secret."),
      el(
        "p",
        {},
        "Les textes et les actes publics sont consultables. Votre bulletin est chiffré sur cet appareil et son admissibilité est prouvée sans joindre votre compte.",
      ),
      link("Vérifier un reçu de vote", "/network/receipt"),
    ),
    cachedNote(),
  );
}
async function proposals() {
  const viewGeneration = generation;

  const kind =
    new URLSearchParams(location.hash.split("?")[1] ?? "").get("type") ?? "";
  let status =
      new URLSearchParams(location.hash.split("?")[1] ?? "").get("status") ??
      "",
    data = await list("proposals", status, kind);
  const select = el("select", {
    id: "status-filter",
    "aria-label": "Filtrer les propositions",
  });
  for (const [value, label] of [
    ["", "Tous les statuts"],
    ["open", "Votes ouverts"],
    ["scheduled", "À venir"],
    ["collecting", "À soutenir"],
    ["applied", "Appliquées"],
    ["rejected", "Rejetées"],
  ]) {
    const o = el("option", { value: value! }, label!);
    o.selected = value === status;
    select.append(o);
  }
  const typeSelect = el("select", { "aria-label": "Filtrer par type" });
  for (const [value, label] of [
    ["", "Tous les types"],
    ["law", "Lois"],
    ["role", "Rôles"],
  ]) {
    const option = el("option", { value: value! }, label!);
    option.selected = value === kind;
    typeSelect.append(option);
  }
  select.onchange = typeSelect.onchange = () => {
    location.hash =
      "/proposals?status=" + select.value + "&type=" + typeSelect.value;
  };
  const cards = el("div", { class: "cards" }, ...data.items.map(card));
  const more = button(
    "Charger la suite",
    async () => {
      const page = await client.get(
        "/v1/proposals?limit=100&status=" +
          encodeURIComponent(status) +
          "&type=" +
          encodeURIComponent(kind) +
          "&cursor=" +
          encodeURIComponent(data.next_cursor),
      );
      for (const p of page.items) cards.append(card(p));
      data = page;
      more.hidden = !page.next_cursor;
    },
    "secondary",
  );
  more.hidden = !data.next_cursor;
  renderPage(
    viewGeneration,
    header(
      "Débattre et décider",
      "Les propositions",
      "Un texte, une action et un calendrier. Chaque modification fait l’objet d’un nouveau vote.",
    ),
    el(
      "div",
      { class: "toolbar" },
      select,
      typeSelect,
      link("+ Nouvelle proposition", "/new/law.create"),
    ),
    cards,
    data.items.length
      ? null
      : el("p", { class: "empty" }, "Aucune proposition dans cette vue."),
    more,
    cachedNote(),
  );
}
async function confirmation(id: string) {
  for (let i = 0; i < 12; i++) {
    const result = await client.confirmation(id);
    if (result.verified) {
      await client.refresh();
      return result;
    }
    await new Promise((y) => setTimeout(y, 500));
  }
  throw Error("ERR_PROOF_PENDING");
}
async function detail(id: string, byPoll = false) {
  const viewGeneration = generation;

  const p = byPoll
    ? await client.value(
        "proposals",
        (await client.get("/v1/polls/" + id)).proposal_id,
      )
    : await client.value("proposals", id);
  if (!p) throw Error("ERR_PROPOSAL");
  const proposalID = byPoll
      ? (await client.get("/v1/polls/" + id)).proposal_id
      : id,
    poll = p.poll_id ? await client.get("/v1/polls/" + p.poll_id) : null;
  const content = el(
    "article",
    { class: "document" },
    el(
      "h2",
      {},
      p.changes?.title ??
        p.changes?.label ??
        actions[p.action] ??
        "Objet de la décision",
    ),
    el(
      "p",
      { class: "body-text" },
      p.changes?.text ??
        p.changes?.description ??
        "Cette décision concerne l’objet " +
          (p.target_id ? short(p.target_id) : "du réseau") +
          ".",
    ),
  );
  const controls = el("div", { class: "actions" });
  if (p.state === "collecting")
    controls.append(
      button(
        "Soutenir cette proposition",
        async () => {
          if (!account) throw Error("ERR_ACCOUNT");
          const tx = await client.publicAction(account!, "proposal.endorse", {
            proposal_id: proposalID,
          });
          await confirmation(tx);
          flash("Soutien enregistré et inclusion vérifiée.");
          await detail(proposalID);
        },
        "secondary",
      ),
    );
  if (poll?.state === "open")
    controls.append(
      button("Participer au vote", async () => votePage(p.poll_id)),
    );
  if (poll?.result)
    controls.append(
      button("Vérifier le résultat", async () => verifyResult(p.poll_id)),
    );
  if (
    development &&
    client.choice.development_clock &&
    ["scheduled", "open", "adopted"].includes(p.state)
  )
    controls.append(
      link(
        "Ouvrir les commandes de l’atelier",
        "/network/workshop?proposal=" + proposalID,
      ),
    );
  renderPage(
    viewGeneration,
    header(actions[p.action] ?? "Proposition", title(p), p.rationale),
    el(
      "div",
      { class: "meta-line" },
      pill(p.state),
      el("span", { class: "verified" }, "✓ Proposition authentifiée"),
    ),
    content,
    el(
      "section",
      { class: "card" },
      el("h2", {}, "Le dossier"),
      el(
        "dl",
        { class: "facts" },
        ...[
          ["Auteur du dépôt public", short(p.author)],
          ["Déposée le", date(p.submitted_ms)],
          ["Soutiens", String(p.endorsements.length)],
          ["Ouverture", date(poll?.opens_at_ms)],
          ["Clôture", date(poll?.closes_at_ms)],
          ["Révision de référence", p.expected_revision ?? "Création"],
          ["Effet prévu", date(p.effective_at_ms)],
        ].flatMap(([k, v]) => [el("dt", {}, k!), el("dd", {}, v!)]),
      ),
    ),
    poll?.result
      ? el(
          "section",
          { class: "result card" },
          el("h2", {}, "Résultat publié"),
          el(
            "div",
            { class: "stats" },
            el("div", {}, el("strong", {}, poll.result.yes), "Oui"),
            el("div", {}, el("strong", {}, poll.result.no), "Non"),
            el(
              "div",
              {},
              el("strong", {}, poll.eligible_count),
              "Comptes admissibles",
            ),
          ),
          el(
            "p",
            {},
            "Participation : " +
              (BigInt(poll.eligible_count) > 0n
                ? String(
                    ((BigInt(poll.result.yes) + BigInt(poll.result.no)) *
                      10000n) /
                      BigInt(poll.eligible_count) /
                      100n,
                  )
                : "0") +
              " %. Certification : " +
              date(poll.result.certified_ms),
          ),
          el(
            "p",
            {},
            "Résultat de l’indexeur : utilisez « Vérifier le résultat » pour contrôler la preuve.",
          ),
        )
      : null,
    p.reason ? el("p", { class: "notice error" }, "Motif : " + p.reason) : null,
    controls,
    el(
      "details",
      {},
      el("summary", {}, "Références et paramètres de décision"),
      el(
        "pre",
        {},
        canonical({
          network_id: client.network,
          proposal_id: proposalID,
          policy: p.policy,
          changes: p.changes,
        }),
      ),
    ),
    cachedNote(),
  );
}
function field(
  label: string,
  name: string,
  value = "",
  options: { area?: boolean; type?: string; required?: boolean } = {},
) {
  const input = options.area
    ? el("textarea", { id: name, name, rows: "7" })
    : el("input", { id: name, name, type: options.type ?? "text" });
  input.value = value;
  if (options.required !== false) input.required = true;
  return el("label", { for: name }, el("span", {}, label), input);
}
async function newProposal(action: string, target?: string) {
  const viewGeneration = generation;

  if (!account) {
    renderPage(
      viewGeneration,
      header(
        "Votre compte",
        "Un compte est nécessaire pour proposer.",
        "La lecture du registre reste publique.",
      ),
      link("Créer ou récupérer un compte", "/account"),
    );
    return;
  }
  const isRole = action.startsWith("role"),
    resource = isRole ? "roles" : "laws",
    object = target ? await client.get("/v1/" + resource + "/" + target) : null,
    form = el("form", { class: "form card" }),
    verb = action.split(".")[1];
  form.append(
    field("Action", "action-name", actions[action] ?? action, {
      required: false,
    }),
  );
  (form.querySelector("#action-name") as HTMLInputElement).readOnly = true;
  if (["create", "amend"].includes(verb!)) {
    form.append(
      field(
        isRole ? "Intitulé du rôle" : "Titre du texte",
        "title",
        object?.current?.content?.title ??
          object?.current?.content?.label ??
          "",
      ),
      field(
        isRole ? "Description" : "Texte intégral proposé",
        "text",
        object?.current?.content?.text ??
          object?.current?.content?.description ??
          "",
        { area: true },
      ),
    );
    if (isRole) {
      const group = el("fieldset", {}, el("legend", {}, "Capacités du rôle"));
      for (const [value, label] of [
        ["annotate_proposal", "Annoter une proposition"],
        ["publish_notice", "Publier un avis"],
        ["report_implementation", "Rendre compte d’une mise en œuvre"],
      ])
        group.append(
          el(
            "label",
            { class: "check" },
            el("input", {
              type: "checkbox",
              name: "capability",
              value: value!,
            }),
            label!,
          ),
        );
      form.append(
        group,
        field(
          "Loi définissant la portée (identifiant, facultatif)",
          "scope",
          "",
          { required: false },
        ),
      );
    }
  }
  if (verb === "restore")
    form.append(field("Révision source à rétablir", "source_revision", "1"));
  if (verb === "grant")
    form.append(
      field(
        "Compte bénéficiaire (identifiant complet)",
        "beneficiary",
        account.account_id,
      ),
      field(
        "Fin de l’attribution (UTC)",
        "until",
        new Date(Number(client.time) + 30 * 86400000)
          .toISOString()
          .slice(0, 16),
        { type: "datetime-local" },
      ),
    );
  if (verb === "revoke")
    form.append(
      field(
        "Identifiant de l’attribution",
        "grant_id",
        new URLSearchParams(location.hash.split("?")[1] ?? "").get("grant") ??
          "",
      ),
    );
  form.append(
    field("Pourquoi proposer cette décision ?", "rationale", "", {
      area: true,
    }),
    el(
      "p",
      { class: "muted" },
      "Le texte et votre compte d’auteur seront publics. L’effet suivra le vote et le délai d’exécution.",
    ),
  );
  const submit = el(
    "button",
    { type: "submit", class: "primary" },
    "Relire avant de déposer",
  );
  form.append(submit);
  form.onsubmit = (event) => {
    event.preventDefault();
    void task(async () => {
      const fd = new FormData(form),
        get = (key: string) =>
          String(fd.get(key) ?? "")
            .normalize("NFC")
            .replace(/\r\n?/g, "\n");
      let changes: Row = {};
      if (["create", "amend"].includes(verb!))
        changes = isRole
          ? {
              label: get("title"),
              description: get("text"),
              capabilities: fd.getAll("capability").map(String).sort(),
              scope_id: get("scope") || null,
            }
          : { title: get("title"), text: get("text"), language: "fr" };
      if (verb === "restore")
        changes = { source_revision: get("source_revision") };
      if (verb === "grant")
        changes = {
          account_id: get("beneficiary"),
          until_ms: String(new Date(get("until") + "Z").getTime()),
        };
      if (verb === "revoke") changes = { grant_id: get("grant_id") };
      const payload = {
        action,
        target_id: target ?? null,
        expected_revision: object?.revision ?? null,
        rationale: get("rationale"),
        requested_effect_ms: null,
        changes,
      };
      validate("submission", payload);
      const preview = el(
        "section",
        { class: "card review" },
        el("h2", {}, "Vérifiez votre proposition"),
        el("h3", {}, actions[action] ?? action),
        el(
          "p",
          { class: "body-text" },
          changes.title ??
            changes.label ??
            (target ? "Objet : " + short(target) : ""),
        ),
        el(
          "p",
          { class: "body-text" },
          changes.text ??
            changes.description ??
            (changes.account_id
              ? "Bénéficiaire : " + changes.account_id
              : changes.source_revision
                ? "Rétablir la révision " + changes.source_revision
                : changes.grant_id
                  ? "Révoquer l’attribution " + short(changes.grant_id)
                  : ""),
        ),
        el("p", {}, "Motif : " + payload.rationale),
        el(
          "details",
          {},
          el("summary", {}, "Références exactes de l’opération"),
          el("pre", {}, canonical(payload)),
        ),
        button("Confirmer le dépôt public", async () => {
          const tx = await client.publicAction(
            account!,
            "proposal.submit",
            payload,
          );
          await confirmation(tx);
          flash("Proposition déposée. Son inclusion est vérifiée.");
          location.hash = "/proposals/" + tx;
        }),
      );
      root.append(preview);
      preview.querySelector("button")!.focus();
      submit.disabled = true;
    });
  };
  renderPage(
    viewGeneration,
    header(
      "Prendre l’initiative",
      actions[action] ?? "Proposer une décision",
      "Une proposition porte une seule action. Sa charge restera fixe après le dépôt.",
    ),
    form,
    cachedNote(),
  );
}
async function objects(resource: "laws" | "roles", id?: string) {
  const viewGeneration = generation;

  if (!id) {
    const data = await list(resource);
    renderPage(
      viewGeneration,
      header(
        "Le registre",
        resource === "laws" ? "Les lois" : "Les rôles civiques",
        resource === "laws"
          ? "Textes, révisions et décisions d’origine, consultables par toutes et tous."
          : "Des fonctions définies par le vote, avec des capacités et une portée explicites.",
      ),
      el(
        "div",
        { class: "toolbar" },
        link(
          resource === "laws" ? "+ Proposer une loi" : "+ Proposer un rôle",
          resource === "laws" ? "/new/law.create" : "/new/role.create",
        ),
      ),
      el(
        "div",
        { class: "cards" },
        ...data.items.map((o) =>
          el(
            "article",
            { class: "card" },
            pill(o.status),
            el(
              "h2",
              {},
              link(
                o.title ??
                  (resource === "laws" ? "Loi " : "Rôle ") +
                    short(String(o.id)),
                "/" + resource + "/" + o.id,
              ),
            ),
            el("p", {}, "Révision " + o.revision),
          ),
        ),
      ),
      data.items.length
        ? null
        : el(
            "p",
            { class: "empty" },
            "Le registre ne contient pas encore de " +
              (resource === "laws" ? "loi." : "rôle."),
          ),
      cachedNote(),
    );
    return;
  }
  const kind = resource === "laws" ? "law" : "role",
    o = await client.value("objects", [kind, id]);
  if (!o) throw Error("ERR_OBJECT");
  const rev = await client.value("revisions", [kind, id, o.revision]);
  if (!rev) throw Error("ERR_REVISION");
  const c = rev.content,
    controls = el("div", { class: "actions" });
  for (const action of o.status === "active"
    ? [kind + ".amend", kind === "law" ? "law.repeal" : "role.retire"]
    : [kind + ".restore"])
    controls.append(link(actions[action]!, "/new/" + action + "?target=" + id));
  const grants = el("section", { class: "card" }, el("h2", {}, "Attributions"));
  if (kind === "role" && o.status === "active") {
    controls.append(link("Attribuer ce rôle", "/new/role.grant?target=" + id));
    const detail = await client.get("/v1/roles/" + id);
    for (const g of detail.grants ?? []) {
      const actual = await client.value("grants", [id, g.id]);
      grants.append(
        el(
          "div",
          { class: "grant" },
          pill(actual.status),
          el(
            "p",
            {},
            "Compte " +
              short(actual.account_id) +
              " · jusqu’au " +
              date(actual.until_ms),
          ),
          actual.status === "active"
            ? link(
                "Proposer la révocation",
                "/new/role.revoke?target=" + id + "&grant=" + g.id,
              )
            : null,
        ),
      );
    }
    if (!detail.grants?.length) grants.append(el("p", {}, "Aucun titulaire."));
  }
  const history = el(
    "section",
    { class: "card" },
    el("h2", {}, "Historique des versions"),
  );
  const detail = await client.get("/v1/" + resource + "/" + id);
  for (const version of detail.versions ?? []) {
    const v = await client.value("revisions", [kind, id, version.revision]);
    history.append(
      el(
        "details",
        {},
        el(
          "summary",
          {},
          "Révision " + version.revision + " · " + date(v.effective_at_ms),
        ),
        el(
          "p",
          { class: "body-text" },
          v.content.text ?? v.content.description ?? "",
        ),
        link("Décision d’origine", "/proposals/" + v.proposal_id),
      ),
    );
  }
  renderPage(
    viewGeneration,
    header(
      resource === "laws" ? "Texte du registre" : "Fonction civique",
      c.title ?? c.label,
      "Révision " + o.revision + " · effet depuis " + date(rev.effective_at_ms),
    ),
    el(
      "div",
      { class: "meta-line" },
      pill(o.status),
      el("span", { class: "verified" }, "✓ Révision et état authentifiés"),
    ),
    el(
      "article",
      { class: "document" },
      el("p", { class: "body-text" }, c.text ?? c.description),
      kind === "role"
        ? el(
            "p",
            {},
            "Capacités : " +
              c.capabilities.join(", ") +
              ". Portée : " +
              (c.scope_id ? short(c.scope_id) : "ensemble du réseau"),
          )
        : null,
    ),
    controls,
    kind === "role" ? grants : null,
    history,
    link(
      "Voir la décision à l’origine de cette version",
      "/proposals/" + rev.proposal_id,
    ),
    cachedNote(),
  );
}
function download(name: string, value: unknown) {
  const url = URL.createObjectURL(
      new Blob([typeof value === "string" ? value : canonical(value)], {
        type: "application/json",
      }),
    ),
    a = el("a", { href: url, download: name });
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
async function accountPage() {
  const viewGeneration = generation;

  const current = account
    ? ((await client.value(
        "accounts",
        account.account_id,
      )) as PublicAccount | null)
    : null;
  const box = el("section", { class: "card form" });
  if (account) {
    box.append(
      el("h2", {}, "Votre compte sur ce réseau"),
      el("p", { class: "identifier" }, account.account_id),
      pill(current?.status ?? "Inscription en attente"),
      el(
        "p",
        {},
        "Clé d’usage et identité de vote conservées dans le coffre chiffré de cet appareil.",
      ),
      button(
        "Télécharger à nouveau la sauvegarde chiffrée",
        async () => {
          const backup = await storedBackup(client.network);
          if (!backup) throw Error("ERR_BACKUP_MISSING");
          download("civora-sauvegarde.json", backup);
        },
        "secondary",
      ),
      button(
        "Effacer le coffre de cet appareil",
        async () => {
          if (
            !confirm(
              "Effacer ce coffre local ? Conservez votre sauvegarde et son code de secours.",
            )
          )
            return;
          await forgetAccount(client.network);
          account = null;
          await accountPage();
        },
        "quiet",
      ),
    );
  } else {
    box.append(
      el("h2", {}, "Un compte, vos clés, votre appareil"),
      el(
        "p",
        {},
        "Aucun email, numéro ni portefeuille externe. Votre compte est propre au réseau actuel.",
      ),
      button("Créer un compte", async () => {
        requireWebCrypto();
        await client.refresh();
        if (!client.canWrite()) throw Error("ERR_OFFLINE_OR_STALE");
        await backupStep(newAccount(client.network));
      }),
    );
  }
  const restore = el(
    "form",
    { class: "form card" },
    el("h2", {}, "Récupérer un compte"),
    el(
      "div",
      {},
      el("label", { for: "recovery-operation" }, "Opération"),
      el(
        "select",
        { id: "recovery-operation" },
        el(
          "option",
          { value: "import" },
          "Récupérer le compte sur cet appareil",
        ),
        el(
          "option",
          { value: "rotate" },
          "Renouveler les clés et l’identité de vote",
        ),
        el(
          "option",
          { value: "retire" },
          "Retirer définitivement le compte du registre",
        ),
      ),
    ),
    field("Fichier de sauvegarde chiffrée", "backup-file", "", {
      type: "file",
    }),
    field("Code de secours (64 caractères hexadécimaux)", "backup-code", "", {
      type: "password",
    }),
    el(
      "button",
      { type: "submit", class: "secondary" },
      "Importer et vérifier le compte",
    ),
  );
  restore.onsubmit = (event) => {
    event.preventDefault();
    void task(async () => {
      const file = (restore.querySelector("#backup-file") as HTMLInputElement)
        .files?.[0];
      if (!file || file.size > 300000) throw Error("ERR_BACKUP_FILE");
      const raw = await file.text(),
        b = parse(raw) as Backup;
      if (b.header.network_id !== client.network) throw Error("ERR_NETWORK");
      await client.refresh();
      const actual = await client.value("accounts", b.header.account_id),
        code = (restore.querySelector("#backup-code") as HTMLInputElement).value
          .trim()
          .toLowerCase(),
        s = await importBackup(raw, code, client.network, actual);
      const operation = (
        restore.querySelector("#recovery-operation") as HTMLSelectElement
      ).value;
      if (operation === "rotate") {
        if (!actual || actual.status !== "active") throw Error("ERR_ACCOUNT");
        const next = newAccount(client.network, s.account_id);
        next.prior_identities = [s.identity, ...s.prior_identities].slice(
          0,
          32,
        );
        (restore.querySelector("#backup-code") as HTMLInputElement).value = "";
        await backupStep(next, { previous: s, recovery: s.recovery_seed });
        return;
      }
      if (operation === "retire") {
        if (
          !actual ||
          !confirm(
            "Retirer ce compte du registre ? Cette opération est définitive.",
          )
        )
          return;
        const tx = await client.publicAction(
          s,
          "account.retire",
          {},
          s.recovery_seed,
        );
        await confirmation(tx);
        (restore.querySelector("#backup-code") as HTMLInputElement).value = "";
        flash(
          "Compte retiré du registre. Les anciens dossiers restent consultables.",
        );
        await accountPage();
        return;
      }
      await saveAccount(s, b);
      account = await loadAccount(client.network);
      (restore.querySelector("#backup-code") as HTMLInputElement).value = "";
      if (!actual) {
        const tx = await client.publicAction(
          account!,
          "account.register",
          credentials(s, s.recovery_seed),
        );
        await confirmation(tx);
      }
      flash("Sauvegarde déchiffrée et compte vérifié sur ce réseau.");
      await accountPage();
    });
  };
  renderPage(
    viewGeneration,
    header(
      "Votre espace personnel",
      "Compte et sauvegarde",
      "Les clés de récupération restent dans votre sauvegarde chiffrée. Conservez séparément son code de secours.",
    ),
    box,
    await notificationControls(),
    restore,
    el(
      "p",
      { class: "source-note" },
      "Le coffre local ne protège pas contre un script malveillant exécuté sous cette origine. Un compte auto-inscrit ne prouve pas l’unicité d’une personne.",
    ),
  );
}
async function backupStep(
  secrets: BackupSecrets,
  rotation?: { previous: LocalAccount; recovery: string },
) {
  const viewGeneration = generation;

  const result = await exportBackup(secrets),
    code = el(
      "code",
      { class: "backup-code", "data-testid": "backup-code" },
      result.code,
    ),
    done = el("input", { id: "backup-saved", type: "checkbox" }),
    confirmButton = button(
      "J’ai conservé la sauvegarde et son code · " +
        (rotation ? "renouveler les clés" : "inscrire le compte"),
      async () => {
        if (!done.checked) throw Error("ERR_BACKUP_NOT_SAVED");
        if (!rotation) {
          await saveAccount(secrets, result.backup);
          account = await loadAccount(client.network);
        }
        code.textContent = "Code masqué";
        const tx = await client.publicAction(
          rotation?.previous ?? account!,
          rotation ? "account.rotate" : "account.register",
          credentials(secrets, secrets.recovery_seed),
          rotation?.recovery,
        );
        await confirmation(tx);
        if (rotation) {
          await saveAccount(secrets, result.backup);
          account = await loadAccount(client.network);
        }
        flash(
          rotation
            ? "Clés renouvelées. L’adresse du compte est conservée et la rotation est vérifiée."
            : "Compte inscrit. L’inclusion de votre inscription est vérifiée.",
        );
        await accountPage();
      },
    );
  confirmButton.disabled = true;
  done.onchange = () => {
    confirmButton.disabled = !done.checked;
  };
  renderPage(
    viewGeneration,
    header(
      rotation
        ? "Renouveler les clés · étape 1 sur 2"
        : "Créer un compte · étape 1 sur 2",
      "Conservez votre moyen de récupération.",
      "La sauvegarde chiffrée et son code sont tous deux nécessaires. Gardez-les dans deux endroits distincts.",
    ),
    el(
      "section",
      { class: "card form" },
      button("Télécharger ma sauvegarde chiffrée", async () =>
        download("civora-sauvegarde.json", result.backup),
      ),
      el("label", {}, "Code de secours", code),
      button(
        "Masquer le code de secours",
        async () => {
          code.textContent = "Code masqué";
        },
        "quiet",
      ),
      el(
        "label",
        { class: "check", for: "backup-saved" },
        done,
        "J’ai sauvegardé le fichier et conservé le code séparément.",
      ),
      confirmButton,
    ),
  );
}
function workerCall(type: string, input: unknown): Promise<any> {
  requireWebCrypto();
  cancelWorker();
  const w = new Worker("/worker.js", { type: "module" });
  activeWorker = w;
  return new Promise((yes, no) => {
    workerReject = no;
    w.onmessage = (e) => {
      w.terminate();
      if (activeWorker === w) activeWorker = null;
      workerReject = null;
      e.data.error ? no(Error(e.data.error)) : yes(e.data.result);
    };
    w.onerror = () => {
      w.terminate();
      workerReject = null;
      no(Error("ERR_CRYPTO_WORKER"));
    };
    w.postMessage({ id: "1", type, input });
  });
}
interface PendingVote {
  raw: string;
  digest: string;
  kind: string;
  tx_id?: string;
  lane_id?: string;
  poll_id: string;
  network_id: string;
}
async function votePage(id: string) {
  const viewGeneration = generation;

  if (!account) {
    location.hash = "/account";
    return;
  }
  const context = await client.context(id),
    v = inspectContext(context),
    p = v.proposal,
    poll = v.poll,
    prior = await pending<PendingVote>(client.network, id);
  if (prior) {
    await receiptPage(id, prior);
    return;
  }
  if (poll.state !== "open" || uint(client.time) >= uint(poll.closes_at_ms))
    throw Error("ERR_PHASE");
  const form = el("form", { class: "card vote" }),
    group = el("fieldset", {}, el("legend", {}, "Votre choix"));
  for (const [value, label] of [
    ["1", "Oui"],
    ["0", "Non"],
  ])
    group.append(
      el(
        "label",
        { class: "vote-option" },
        el("input", {
          type: "radio",
          name: "choice",
          value: value!,
          required: "",
        }),
        el("strong", {}, label!),
        el(
          "span",
          {},
          value === "1"
            ? "J’approuve cette proposition"
            : "Je n’approuve pas cette proposition",
        ),
      ),
    );
  form.append(
    group,
    el("button", { type: "submit", class: "primary" }, "Vérifier mon choix"),
  );
  form.onsubmit = (event) => {
    event.preventDefault();
    const choice = Number(new FormData(form).get("choice")) as 0 | 1;
    const confirmBox = el(
      "section",
      { class: "card review" },
      el(
        "h2",
        {},
        "Vous avez choisi « " + (choice === 1 ? "Oui" : "Non") + " »",
      ),
      el("p", {}, title(p) + " · " + client.choice.name),
      el(
        "p",
        {},
        "Ce choix sera chiffré sur cet appareil. Aucun identifiant de compte ne sera joint au bulletin.",
      ),
      button("Confirmer et chiffrer mon bulletin", async () => {
        const generationAtStart = generation,
          c = client,
          a = account!;
        await c.refresh();
        if (!c.canWrite()) throw Error("ERR_OFFLINE_OR_STALE");
        const fresh = await c.context(id),
          inspected = inspectContext(fresh);
        if (
          inspected.poll.state !== "open" ||
          uint(c.time) >= uint(inspected.poll.closes_at_ms)
        )
          throw Error("ERR_PHASE");
        let identity = a.identity,
          path;
        for (const candidate of [a.identity, ...a.prior_identities]) {
          try {
            const commitment = Identity.import(candidate).commitment.toString();
            path = await c.get(
              "/v1/polls/" + id + "/membership-path?commitment=" + commitment,
              false,
            );
            identity = candidate;
            break;
          } catch {}
        }
        if (!path) throw Error("ERR_NOT_ELIGIBLE");
        flash(
          "Chiffrement et preuve d’admissibilité en cours sur cet appareil…",
        );
        const result = await workerCall("vote", {
          context: fresh,
          identity,
          path,
          choice,
        });
        if (generation !== generationAtStart) throw Error("ERR_NETWORK");
        const value: PendingVote = {
          ...result,
          network_id: c.network,
          poll_id: id,
        };
        await encryptPending(c.network, id, value);
        await transmit(id, value);
        await receiptPage(id, value);
      }),
    );
    form.replaceWith(confirmBox);
    confirmBox.querySelector("button")!.focus();
  };
  renderPage(
    viewGeneration,
    header("Participer au scrutin", title(p), p.changes?.text ?? p.rationale),
    el(
      "div",
      { class: "network-reminder" },
      client.choice.name +
        " · " +
        short(client.network) +
        " · clôture " +
        date(poll.closes_at_ms),
    ),
    form,
    el(
      "p",
      { class: "source-note" },
      "Votre compte doit figurer dans l’instantané d’ouverture. Le vote est secret ; le vote à distance ne protège pas contre la coercition ou un appareil compromis.",
    ),
  );
}
async function transmit(id: string, value: PendingVote) {
  if (value.network_id !== client.network || value.poll_id !== id)
    throw Error("ERR_NETWORK");
  await client.refresh();
  const c = await client.context(id),
    v = inspectContext(c);
  if (v.poll.state !== "open" || uint(client.time) >= uint(v.poll.closes_at_ms))
    throw Error("ERR_PHASE");
  await client.post(
    value.kind === "partition"
      ? "/v1/lanes/" + value.lane_id + "/ballots"
      : "/v1/transactions",
    value.raw,
  );
}
async function receiptPage(id: string, value: PendingVote) {
  const viewGeneration = generation;

  let status = "En cours de transmission",
    verified = false,
    result: Row = {};
  try {
    if (value.kind === "inline") {
      result = await client.confirmation(value.tx_id!);
      verified = !!result.verified;
      if (verified) status = "Compté · inclusion vérifiée";
    } else {
      result = await client.get(
        "/v1/lanes/" + value.lane_id + "/receipts/" + value.digest,
        false,
      );
      if (result.status === "replicated") {
        const f = (await client.context(id)).frozen!.path.entry.value as Row,
          header = verifyHeader(
            f.lanes[Number(value.lane_id)],
            result.proof.signed_header,
          ),
          native = JSON.parse(blob(result.proof.transaction_proof).toString()),
          data = Buffer.from(native.data, "base64"),
          ref = result.reference;
        if (native.root_hash.toLowerCase() !== header.data_hash.toLowerCase())
          throw Error("ERR_PROOF");
        verifyInclusion(
          header.data_hash.toLowerCase(),
          sha(data),
          uint(native.proof.index),
          uint(native.proof.total),
          native.proof.aunts.map((s: string) =>
            Buffer.from(s, "base64").toString("hex"),
          ),
        );
        verifyInclusion(
          ref.root,
          Buffer.from(value.raw),
          uint(result.batch_index),
          uint(ref.count),
          result.inclusion,
        );
        const batch = parse(data) as Row;
        if (
          batch.type !== "partition.admit" ||
          batch.value.poll_id !== id ||
          !batch.value.envelopes.includes(
            Buffer.from(value.raw).toString("base64url"),
          )
        )
          throw Error("ERR_PROOF_CONTEXT");
        const envelopes = batch.value.envelopes.map((s: string) => blob(s));
        if (
          header.height !== result.height ||
          Date.parse(header.time) < Number(f.opens_at_ms) ||
          Date.parse(header.time) >= Number(f.closes_at_ms) ||
          ref.network_id !== client.network ||
          ref.poll_id !== id ||
          ref.lane_id !== value.lane_id ||
          ref.protocol_epoch !== f.context.protocol_epoch ||
          ref.count !== String(envelopes.length) ||
          ref.root !== logRoot(envelopes) ||
          ref.manifest_sha256 !==
            sha(
              Buffer.from(
                canonical(
                  envelopes.map((b: Uint8Array) => ({
                    sha256: sha(b).toString("hex"),
                    size: String(b.length),
                  })),
                ),
              ),
            ).toString("hex") ||
          uint(result.index) !== uint(ref.start) + uint(result.batch_index)
        )
          throw Error("ERR_PROOF_CONTEXT");
        verifyArchiveCertificate(f as Frozen, ref, batch.value.certificate);
        verified = true;
        status = "Déposé avant la clôture · vérification en cours";
        if (result.anchor) {
          const accepted = checkProof(
            client.genesis!,
            result.anchor,
            "transport_checkpoints",
            [id, value.lane_id],
          ) as Row;
          if (accepted.summary.processing_sealed && result.status_witness) {
            verifyWitness(accepted.summary.status_root, result.status_witness);
            const w = result.status_witness;
            if (
              !w.exists ||
              w.entry.table !== "status" ||
              w.entry.key !== result.index ||
              w.entry.value.digest !== value.digest
            )
              throw Error("ERR_PROOF_CONTEXT");
            status =
              w.entry.value.status === "counted"
                ? "Compté · scellé principal disponible"
                : "Statut publié : " + w.entry.value.status;
          }
        }
      }
    }
  } catch (e) {
    if (!navigator.onLine || client.offline)
      status = "Reçu conservé sur cet appareil · hors ligne";
    else throw e;
  }
  renderPage(
    viewGeneration,
    header(
      "Votre bulletin",
      status,
      "Le même bulletin chiffré est conservé sur cet appareil. Une retransmission réutilise exactement les mêmes octets.",
    ),
    el(
      "section",
      { class: "card" },
      el(
        "p",
        { class: verified ? "verified" : "muted" },
        verified
          ? "✓ Preuve d’inclusion contrôlée"
          : "Un accusé de dépôt ne constitue pas encore une inclusion.",
      ),
      el("p", { class: "identifier" }, value.digest),
      el(
        "div",
        { class: "actions" },
        button(
          "Actualiser le reçu",
          async () => receiptPage(id, value),
          "secondary",
        ),
        button(
          "Retransmettre le même bulletin",
          async () => {
            await transmit(id, value);
            await receiptPage(id, value);
          },
          "secondary",
        ),
        button(
          "Télécharger mon reçu",
          async () =>
            download("civora-recu.json", {
              network_id: client.network,
              poll_id: id,
              ballot: value,
              ...(result ? { receipt: result } : {}),
            }),
          "quiet",
        ),
      ),
    ),
    link("Retour au scrutin", "/polls/" + id),
    cachedNote(),
  );
}
async function verifyResult(id: string) {
  const viewGeneration = generation;

  flash("Vérification indépendante du résultat en cours…");
  let report: Row;
  if (client.config.voting_profile === "transport_dev_v0_6") {
    const index = await client.get("/v1/polls/" + id + "/record", false),
      fetchObject = async (ref: Row) => {
        let last;
        for (const base of client.choice.api_roots) {
          try {
            const r = await fetch(base + "/v1/objects/" + ref.sha256, {
              credentials: "omit",
              cache: "no-store",
            });
            const data = new Uint8Array(await r.arrayBuffer());
            if (
              !r.ok ||
              String(data.length) !== ref.size ||
              sha(data).toString("hex") !== ref.sha256
            )
              throw Error("ERR_OBJECT_HASH");
            return data;
          } catch (e) {
            last = e;
          }
        }
        throw last;
      };
    validate("citizen_record_index", index);
    if (index.poll_id !== id || index.network_id !== client.network)
      throw Error("ERR_NETWORK");
    const record = parse(await fetchObject(index.record)) as Row;
    validate("citizen_election_record", record);
    if (record.genesis.sha256 !== client.choice.manifest.main.genesis.sha256)
      throw Error("ERR_GENESIS");
    const refs = new Map<string, Row>([
      [record.genesis.sha256, record.genesis],
    ]);
    for (const lane of record.lanes)
      for (const batch of lane.batches)
        for (const ref of batch.envelopes) refs.set(ref.sha256, ref);
    const objects = [];
    for (const ref of refs.values())
      objects.push([ref.sha256, await fetchObject(ref)]);
    report = await workerCall("audit", {
      record,
      objects,
      genesis: client.choice.manifest.main.genesis.sha256,
    });
  } else {
    const context = await client.context(id),
      t = await client.get("/v1/polls/" + id + "/tally", false),
      raw = verifyTransaction(
        client.genesis!,
        t.publication,
        t.publication.tx_id,
      ),
      tx = parse(raw) as Row;
    if (
      tx.body.type !== "poll.tally" ||
      tx.body.payload.poll_id !== id ||
      tx.body.payload.decryption_record !== t.decryption_record
    )
      throw Error("ERR_RESULT_CONTEXT");
    report = {
      ...(await workerCall("result", { context, tally: t.decryption_record })),
      as_of_height: context.poll!.height,
    };
  }
  flash("Preuve du total vérifiée sur cet appareil.");
  renderPage(
    viewGeneration,
    header(
      "Vérification indépendante",
      "Le résultat est vérifié.",
      "Le total chiffré, sa preuve de déchiffrement et son contexte authentifié concordent.",
    ),
    el(
      "section",
      { class: "card result" },
      el(
        "div",
        { class: "stats" },
        el("div", {}, el("strong", {}, report.yes), "Oui"),
        el("div", {}, el("strong", {}, report.no), "Non"),
      ),
      el(
        "p",
        {},
        "Vérifié à la hauteur " +
          report.as_of_height +
          ". Cette preuve historique ne garantit pas l’absence d’une contestation ultérieure.",
      ),
      button(
        "Télécharger le rapport",
        async () =>
          download("civora-verification.json", {
            network_id: client.network,
            poll_id: id,
            ...report,
          }),
        "secondary",
      ),
    ),
    link("Retour au dossier", "/polls/" + id),
  );
}
async function notificationControls(): Promise<HTMLElement> {
  const box = el(
      "section",
      { class: "card form" },
      el("h2", {}, "Notifications"),
    ),
    state = await notificationSettings(),
    capability = pushCapability(),
    group = el("fieldset", {}, el("legend", {}, "Alertes des scrutins"));
  box.append(
    el(
      "p",
      {},
      "Recevez une alerte générique à l’ouverture d’un scrutin. L’abonnement est indépendant du compte et ne contient aucun bulletin. La boîte d’actualités fonctionne sans autorisation de notification.",
    ),
  );
  for (const choice of choices) {
    const input = el("input", {
      type: "checkbox",
      name: "follow-network",
      value: choice.manifest.network_id,
    });
    input.checked = state
      ? state.networks.includes(choice.manifest.network_id)
      : choice.manifest.network_id === client.network;
    group.append(
      el("label", { class: "check" }, input, "Suivre l’ouverture des votes"),
    );
  }
  const selected = () =>
    [...group.querySelectorAll<HTMLInputElement>("input:checked")].map(
      (i) => i.value,
    );
  box.append(
    group,
    el(
      "p",
      { class: "muted" },
      state?.enabled
        ? "Push activés sur cet appareil."
        : "Push désactivés. Les actualités restent accessibles.",
    ),
  );
  if (capability) box.append(el("p", { class: "source-note" }, capability));
  if (!notificationRoot)
    box.append(
      el(
        "p",
        {},
        "Le service de notifications n’est pas configuré. Vous pouvez consulter les dossiers et voter normalement.",
      ),
    );
  else {
    if (!state?.enabled && !state?.pending_delete && !capability)
      box.append(
        button("Activer les notifications", async () => {
          const networks = selected();
          if (!networks.length)
            throw Error("Activez les alertes des scrutins.");
          await enablePush(notificationRoot!, networks);
          flash(
            "Notifications activées. Aucun identifiant de compte n’est transmis au service.",
          );
          await route();
        }),
      );
    if (state?.enabled)
      box.append(
        button(
          "Enregistrer mes préférences",
          async () => {
            await updatePreferences(selected());
            flash("Préférences enregistrées.");
            await route();
          },
          "secondary",
        ),
      );
    if (state)
      box.append(
        button(
          state.pending_delete
            ? "Réessayer la désinscription"
            : "Désactiver et me désabonner",
          async () => {
            const removed = await disablePush();
            flash(
              removed
                ? "Abonnement supprimé. Les actualités restent consultables."
                : "Push désactivés sur cet appareil. La suppression côté serveur reste à réessayer lorsque la connexion sera disponible.",
            );
            await route();
          },
          "secondary",
        ),
      );
  }
  box.append(link("Ouvrir la boîte d’actualités", "/news"));
  return box;
}
async function newsPage() {
  const viewGeneration = generation;

  const network = client.network,
    key = "news/" + network,
    seenKey = "news-read/" + network;
  let items: Row[] = [],
    cursor = "",
    cached = false,
    source = true,
    time = Date.now();
  const seen = new Set((await read<string[]>("settings", seenKey)) ?? []);
  if (notificationRoot)
    try {
      const page = await getNews(notificationRoot, network);
      items = page.items;
      cursor = page.next_cursor;
      source = page.source_available;
      await write("public", key, { items, cursor, source, time });
    } catch {
      const old = await read<{
        items: Row[];
        cursor: string;
        source: boolean;
        time: number;
      }>("public", key);
      if (old) {
        ({ items, cursor, source, time } = old);
        cached = true;
      } else source = false;
    }
  const cards = el("div", { class: "cards" });
  const card = (item: Row) =>
    el(
      "article",
      { class: "card" },
      el("p", { class: "eyebrow" }, seen.has(item.event_id) ? "Lu" : "Nouveau"),
      el("h2", {}, "Un référendum a été ouvert"),
      el("p", {}, "Ouverture : " + date(item.opened_at_ms)),
      el("p", {}, "Clôture : " + date(item.closes_at_ms)),
      el(
        "p",
        {},
        uint(item.closes_at_ms) > BigInt(Date.now())
          ? "Consultez le dossier et son calendrier avant de participer."
          : "Cette annonce est historique. Vérifiez le statut actuel du dossier.",
      ),
      link(
        "Consulter la question",
        "/polls/" + item.poll_id + "?network=" + network,
      ),
    );
  for (const item of items) cards.append(card(item));
  const more = button(
    "Charger les actualités suivantes",
    async () => {
      const page = await getNews(notificationRoot!, network, cursor);
      for (const item of page.items) {
        items.push(item);
        cards.append(card(item));
      }
      cursor = page.next_cursor;
      more.hidden = !cursor;
      await write("public", key, {
        items,
        cursor,
        source: page.source_available,
        time: Date.now(),
      });
    },
    "secondary",
  );
  more.hidden = !cursor || cached;
  renderPage(
    viewGeneration,
    header(
      "Suivre le registre",
      "Les actualités",
      "Les ouvertures de scrutin restent consultables, avec ou sans Push et sans créer de compte.",
    ),
    el(
      "p",
      { class: "source-note" },
      cached
        ? "Actualités en cache · dernière collecte : " + date(String(time))
        : source
          ? "Annonces collectées après vérification des ouvertures finalisées. Ouvrez le dossier pour consulter les preuves et le statut courant."
          : "La source est indisponible ou le service n’est pas configuré. Les annonces conservées restent consultables.",
    ),
    cards,
    items.length
      ? null
      : el(
          "p",
          { class: "empty" },
          "Aucune ouverture de scrutin annoncée pour ce réseau.",
        ),
    more,
    button(
      "Marquer les annonces affichées comme lues",
      async () => {
        await write(
          "settings",
          seenKey,
          [...new Set([...seen, ...items.map((i) => i.event_id)])].slice(-1000),
        );
        await newsPage();
      },
      "quiet",
    ),
    await notificationControls(),
  );
}
async function verification() {
  const viewGeneration = generation;

  const file = el("input", {
      type: "file",
      accept: ".json,application/json",
      id: "receipt-file",
      required: "",
    }),
    form = el(
      "form",
      { class: "card form" },
      el("h2", {}, "Vérifier un reçu téléchargé"),
      el("label", { for: "receipt-file" }, "Fichier du reçu", file),
      el(
        "p",
        {},
        "Le reçu sera comparé aux preuves du réseau actuel. Aucun bulletin ne sera envoyé.",
      ),
      el("button", { type: "submit", class: "secondary" }, "Vérifier ce reçu"),
    );
  form.onsubmit = (event) => {
    event.preventDefault();
    void task(async () => {
      const selected = file.files?.[0];
      if (!selected || selected.size > 8 * 1024 * 1024)
        throw Error("ERR_CAPACITY");
      const imported = parse(Buffer.from(await selected.text())) as Row;
      const v = imported.ballot as PendingVote;
      if (
        !v ||
        imported.network_id !== client.network ||
        v.network_id !== client.network ||
        imported.poll_id !== v.poll_id
      )
        throw Error("ERR_NETWORK");
      if (
        typeof v.raw !== "string" ||
        sha(Buffer.from(v.raw)).toString("hex") !== v.digest
      )
        throw Error("ERR_RECEIPT");
      const tx = parse(Buffer.from(v.raw)) as Row;
      if (v.kind === "inline") {
        validate("transaction", tx);
        if (
          tx.body.type !== "vote.cast" ||
          tx.body.actor_id !== null ||
          tx.body.network_id !== client.network ||
          tx.body.payload.poll_id !== v.poll_id ||
          domain(tx.body.v === "1" ? "tx" : "tx/2", tx.body) !== v.tx_id
        )
          throw Error("ERR_PROOF_CONTEXT");
      } else if (v.kind === "partition") {
        validate("scale_envelope", tx);
        if (
          tx.network_id !== client.network ||
          tx.poll_id !== v.poll_id ||
          tx.lane_id !== v.lane_id
        )
          throw Error("ERR_PROOF_CONTEXT");
      } else throw Error("ERR_RECEIPT");
      await receiptPage(v.poll_id, v);
    });
  };
  renderPage(
    viewGeneration,
    header(
      "Votre vote",
      "Vérifier un reçu de vote.",
      "Importez le reçu téléchargé après votre vote pour contrôler son inclusion dans le registre. La vérification s’effectue sur cet appareil.",
    ),
    form,
    el(
      "p",
      { class: "source-note" },
      "Pour consulter les blocs et les preuves d’état du registre, ouvrez l’explorateur.",
    ),
    link("Ouvrir l’explorateur", "/network/explorer"),
  );
}
function explorerTabs(state = false) {
  const tabs = el(
    "nav",
    { class: "explorer-tabs", "aria-label": "Contenu de l’explorateur" },
    link("Blocs", "/network/explorer"),
    link("États du registre", "/network/explorer/state"),
  );
  [...tabs.children].forEach((child, i) =>
    child.setAttribute("aria-current", (i === 1) === state ? "page" : "false"),
  );
  return tabs;
}

async function explorerBlocksPage(query: URLSearchParams) {
  const viewGeneration = generation,
    latest = String(uint(client.height) + 1n);
  const getBlock = async (height: string) => {
    let inspected: ExplorerBlock | undefined;
    await client.get("/v1/blocks/" + height, true, (value) => {
      inspected = inspectExplorerBlock(client.genesis!, value, height);
    });
    return inspected!;
  };
  const heightInput = el("input", {
    id: "explorer-block-height",
    type: "text",
    inputmode: "numeric",
    pattern: "[1-9][0-9]*",
    required: "",
    maxlength: "20",
    placeholder: "Ex. 12",
  });
  const search = el(
    "form",
    { class: "explorer-search" },
    el(
      "label",
      { for: "explorer-block-height" },
      "Numéro du bloc",
      heightInput,
    ),
    el("button", { type: "submit", class: "secondary" }, "Consulter le bloc"),
  );
  search.onsubmit = (event) => {
    event.preventDefault();
    void task(async () => {
      const h = explorerHeight(heightInput.value.trim(), latest);
      location.hash = "/network/explorer?height=" + h;
    });
  };
  const note = el(
    "p",
    { class: "source-note" },
    "Les en-têtes signés et les octets des transactions affichées sont vérifiés sur cet appareil. L’explorateur ne rejoue pas l’exécution complète du registre ; l’observateur effectue ce contrôle. Cet atelier utilise des commits synthétiques TEST_ONLY.",
  );
  const requested = query.get("height");
  if (requested) {
    const height = explorerHeight(requested, latest),
      block = await getBlock(height),
      h = block.header;
    heightInput.value = height;
    renderPage(
      viewGeneration,
      header(
        "Explorateur · blocs",
        "Bloc " + height,
        "Consultez l’en-tête, les empreintes et les transactions publiques de ce bloc.",
      ),
      explorerTabs(),
      search,
      el(
        "p",
        { class: "verified", "data-block-verified": height },
        "✓ En-tête signé et données des transactions vérifiés",
      ),
      el(
        "section",
        { class: "card" },
        el(
          "dl",
          { class: "facts" },
          ...[
            ["Hauteur", height],
            ["Date du bloc", h.time],
            ["Empreinte du bloc", block.hash],
            ["Bloc précédent", h.last_block_id.hash || "Genèse"],
            ["Racine des transactions", h.data_hash],
            ["Racine d’état avant ce bloc", h.app_hash || "État initial"],
            ["Transactions", String(block.transactions.length)],
            ["Signatures du commit", String(block.signatures) + " / 7"],
          ].flatMap(([name, value]) => [
            el("dt", {}, name!),
            el("dd", { class: "identifier" }, value!),
          ]),
        ),
      ),
      el(
        "p",
        { class: "source-note" },
        "La racine d’état de l’en-tête du bloc H correspond à l’état après le bloc H−1. Consultez l’onglet États du registre pour obtenir une preuve d’état ancrée au bloc suivant.",
      ),
      el(
        "section",
        {},
        el("h2", {}, "Transactions publiques"),
        ...block.transactions
          .slice(0, 20)
          .map((tx, i) =>
            el(
              "article",
              { class: "card" },
              el("h3", {}, "Transaction " + (i + 1)),
              el("p", {}, tx.type),
              el(
                "p",
                { class: "identifier" },
                "SHA-256 des octets : " + tx.sha256,
              ),
              el(
                "details",
                {},
                el("summary", {}, "Voir les données publiques"),
                el("pre", {}, JSON.stringify(tx.value, null, 2)),
              ),
            ),
          ),
        !block.transactions.length
          ? el(
              "p",
              { class: "empty" },
              "Ce bloc ne contient aucune transaction.",
            )
          : null,
        block.transactions.length > 20
          ? el(
              "p",
              { class: "source-note" },
              "Les 20 premières transactions sont affichées. L’empreinte a été contrôlée sur toutes les transactions du bloc.",
            )
          : null,
      ),
      el(
        "div",
        { class: "actions" },
        uint(height) > 1n
          ? link(
              "Bloc précédent",
              "/network/explorer?height=" + (uint(height) - 1n),
            )
          : null,
        uint(height) < uint(latest)
          ? link(
              "Bloc suivant",
              "/network/explorer?height=" + (uint(height) + 1n),
            )
          : null,
        link("Blocs récents", "/network/explorer"),
      ),
      note,
    );
    return;
  }
  const before = query.get("before"),
    top = before ? uint(explorerHeight(before, latest)) : uint(latest);
  const heights: string[] = [];
  for (let h = top; h > 0n && heights.length < 10; h--) heights.push(String(h));
  const blocks = await Promise.all(heights.map(getBlock));
  for (let i = 0; i + 1 < blocks.length; i++)
    if (
      blocks[i]!.header.last_block_id.hash.toLowerCase() !== blocks[i + 1]!.hash
    )
      throw Error("ERR_PROOF_BLOCK_LINK");
  renderPage(
    viewGeneration,
    header(
      "Explorateur",
      "Les blocs du registre.",
      "Parcourez les blocs récents ou recherchez un bloc par son numéro. Les états publics sont accessibles dans l’onglet voisin.",
    ),
    explorerTabs(),
    search,
    el(
      "div",
      { class: "explorer-blocks" },
      ...blocks.map((block) =>
        el(
          "article",
          { class: "card explorer-block" },
          el(
            "h2",
            {},
            link(
              "Bloc " + block.header.height,
              "/network/explorer?height=" + block.header.height,
            ),
          ),
          el("p", {}, date(String(Date.parse(block.header.time)))),
          el("p", {}, block.transactions.length + " transaction(s)"),
          el("p", { class: "identifier" }, block.hash),
          el(
            "span",
            { class: "verified" },
            "✓ En-tête et transactions vérifiés",
          ),
        ),
      ),
    ),
    el(
      "div",
      { class: "actions" },
      uint(heights.at(-1)!) > 1n
        ? link(
            "Blocs plus anciens",
            "/network/explorer?before=" + (uint(heights.at(-1)!) - 1n),
          )
        : null,
      before ? link("Derniers blocs", "/network/explorer") : null,
    ),
    note,
    cachedNote(),
  );
}

async function explorerStatePage(query: URLSearchParams) {
  const viewGeneration = generation;
  const kinds = [
    ["network", "Configuration du réseau"],
    ["accounts", "Compte"],
    ["proposals", "Proposition"],
    ["polls", "Scrutin"],
    ["law", "Loi"],
    ["role", "Rôle"],
  ];
  const kind = el(
    "select",
    { id: "explorer-state-kind" },
    ...kinds.map(([value, name]) => el("option", { value: value! }, name!)),
  );
  kind.value = kinds.some(([value]) => value === query.get("type"))
    ? query.get("type")!
    : "network";
  const key = el("input", {
    id: "explorer-state-key",
    type: "text",
    maxlength: "64",
    placeholder: "Identifiant public en hexadécimal",
  });
  key.value = query.get("id") ?? "";
  const height = el("input", {
    id: "explorer-state-height",
    type: "text",
    inputmode: "numeric",
    pattern: "[1-9][0-9]*",
    maxlength: "20",
    required: "",
  });
  height.value = explorerHeight(
    query.get("height") ?? client.height,
    client.height,
  );
  const result = el("section", {
    class: "card",
    "aria-label": "Résultat de la preuve d’état",
  });
  const updateKind = () => {
    key.disabled = kind.value === "network";
    key.required = !key.disabled;
    if (key.disabled) key.value = "config";
    else if (key.value === "config") key.value = "";
  };
  updateKind();
  kind.onchange = () => {
    updateKind();
    result.replaceChildren(
      el("p", {}, "Consultez l’état pour vérifier votre nouvelle sélection."),
    );
  };
  const inspect = async () => {
    result.replaceChildren(el("p", {}, "Vérification de la preuve en cours…"));
    const selectedHeight = explorerHeight(height.value.trim(), client.height),
      selectedKind = kind.value;
    const id = key.value.trim().toLowerCase();
    if (selectedKind !== "network" && !/^[0-9a-f]{64}$/.test(id))
      throw Error("ERR_EXPLORER_KEY");
    const table = ["law", "role"].includes(selectedKind)
      ? "objects"
      : selectedKind;
    const stateKey =
      table === "objects"
        ? [selectedKind, id]
        : selectedKind === "network"
          ? "config"
          : id;
    const proof = await client.proof(table, stateKey, selectedHeight);
    if (generation !== viewGeneration) return;
    result.replaceChildren(
      el("h2", {}, proof.path.exists ? "État vérifié" : "Absence vérifiée"),
      el(
        "p",
        { class: "verified", "data-state-proof-height": selectedHeight },
        "✓ Preuve d’état contrôlée sur cet appareil",
      ),
      el(
        "dl",
        { class: "facts" },
        ...[
          ["Table", table],
          ["Clé", JSON.stringify(stateKey)],
          ["État après le bloc", selectedHeight],
          ["Ancré par l’en-tête du bloc", String(uint(selectedHeight) + 1n)],
        ].flatMap(([name, value]) => [
          el("dt", {}, name!),
          el("dd", { class: "identifier" }, value!),
        ]),
      ),
      proof.path.exists
        ? el(
            "details",
            {},
            el("summary", {}, "Voir la valeur publique"),
            el("pre", {}, JSON.stringify(proof.path.entry.value, null, 2)),
          )
        : el(
            "p",
            {},
            "Cette clé est absente du registre à la hauteur choisie.",
          ),
      button(
        "Télécharger la preuve d’état",
        async () => {
          download("civora-preuve-etat-" + selectedHeight + ".json", {
            network_id: client.network,
            genesis_sha256: client.choice.manifest.main.genesis.sha256,
            proof,
          });
        },
        "secondary",
      ),
    );
  };
  const form = el(
    "form",
    { class: "card form" },
    el("label", { for: "explorer-state-kind" }, "Type d’état", kind),
    el("label", { for: "explorer-state-key" }, "Identifiant public", key),
    el("label", { for: "explorer-state-height" }, "Hauteur de l’état", height),
    el(
      "p",
      { class: "source-note" },
      "La hauteur correspond à l’état après ce bloc. Collez un identifiant public de compte, proposition, scrutin, loi ou rôle pour consulter sa valeur ou vérifier son absence.",
    ),
    el("button", { type: "submit", class: "secondary" }, "Consulter l’état"),
  );
  form.onsubmit = (event) => {
    event.preventDefault();
    void task(inspect);
  };
  renderPage(
    viewGeneration,
    header(
      "Explorateur · états",
      "L’état du registre, avec sa preuve.",
      "Consultez une valeur publique à une hauteur donnée. Chaque résultat vérifie son contexte et sa preuve cryptographique, y compris pour une clé absente.",
    ),
    explorerTabs(true),
    form,
    result,
    el(
      "p",
      { class: "source-note" },
      "Une preuve de l’état après le bloc H est authentifiée par l’en-tête signé du bloc H+1. Les coffres, sauvegardes et choix de vote de cet appareil ne sont pas consultés par cet explorateur.",
    ),
  );
  await inspect();
}

async function workshopPage(proposalID: string | null) {
  if (!development || !client.choice.development_clock)
    throw Error("ERR_PROFILE");
  const proposals = (await list("proposals")).items.filter(
    (p) =>
      ["scheduled", "open", "adopted"].includes(p.state) &&
      (!proposalID || p.id === proposalID),
  );
  renderPage(
    generation,
    header(
      "Atelier local",
      "Commandes de démonstration.",
      "Ces commandes font avancer l’horloge de développement pour ouvrir, clore et appliquer les scrutins. Elles sont propres à cet atelier expérimental.",
    ),
    ...proposals.map((p) =>
      el(
        "article",
        { class: "card" },
        el("h2", {}, title(p)),
        pill(p.state),
        el(
          "div",
          { class: "actions" },
          button(
            p.state === "scheduled"
              ? "Atelier : ouvrir le vote"
              : p.state === "open"
                ? "Atelier : clore et dépouiller"
                : "Atelier : appliquer la décision",
            async () => {
              const response = await fetch("/__dev/advance", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: canonical({ id: p.id }),
              });
              if (!response.ok) throw Error("ERR_DEVELOPMENT");
              await route();
            },
            "development",
          ),
          link("Retour au dossier", "/proposals/" + p.id),
        ),
      ),
    ),
    !proposals.length
      ? el(
          "p",
          { class: "empty" },
          "Aucun scrutin n’attend une commande de l’atelier.",
        )
      : null,
    proposalID && !proposals.length
      ? link("Retour au dossier", "/proposals/" + proposalID)
      : null,
  );
}

async function networkPage() {
  const viewGeneration = generation;
  const counts = ["active_nodes", "observer_nodes", "citizen_accounts"];
  const stats = counts.map((key) =>
    el("strong", { "data-network-stat": key }, "—"),
  );
  const observed = el(
    "p",
    { class: "source-note", role: "status" },
    "Chargement de l’activité…",
  );
  const refresh = async () => {
    if (!participationAvailable) {
      observed.textContent =
        "Le suivi des nœuds n’est pas activé sur cet hôte.";
      return;
    }
    try {
      const response = await fetch("/network/overview", {
        cache: "no-store",
        redirect: "error",
        signal: AbortSignal.timeout(15000),
      });
      if (!response.ok) throw Error("ERR_NETWORK_OVERVIEW");
      const data = await response.json();
      if (
        data.format !== "civora-network-overview-1" ||
        data.network_id !== client.network ||
        counts.some((key) => !/^(0|[1-9][0-9]*)$/.test(data[key]))
      )
        throw Error("ERR_NETWORK_OVERVIEW");
      if (generation !== viewGeneration) return;
      stats.forEach((node, i) => {
        node.textContent = data[counts[i]!];
      });
      observed.textContent =
        "Mis à jour à " +
        new Date(Number(data.observed_at)).toLocaleTimeString("fr-FR") +
        ". Un serveur d’atelier et les observateurs annoncés depuis moins de 45 secondes. " +
        data.test_accounts +
        " comptes de démonstration exclus du nombre de comptes citoyens. Actualisation toutes les 15 secondes.";
    } catch {
      if (generation !== viewGeneration) return;
      stats.forEach((node) => {
        node.textContent = "—";
      });
      observed.textContent =
        "L’activité est momentanément indisponible. Aucun nombre de participants ne peut être confirmé.";
    }
  };
  renderPage(
    viewGeneration,
    header(
      "Le réseau",
      "Un registre partagé, des ordinateurs qui le vérifient.",
      "Retrouvez ici l’activité du réseau actuel, les téléchargements et les informations techniques. Aucun compte n’est nécessaire pour faire tourner un nœud observateur.",
    ),
    el(
      "section",
      { class: "stats", "aria-label": "Participation au réseau" },
      ...["Nœuds actifs", "Dont observateurs", "Comptes citoyens actifs"].map(
        (label, i) => el("div", {}, stats[i]!, el("span", {}, label)),
      ),
    ),
    observed,
    el(
      "p",
      { class: "network-health" },
      "Les présences sont déclarées par les nœuds et vérifiées par le serveur de l’atelier. Elles ne mesurent ni des personnes distinctes, ni l’indépendance des machines.",
    ),
    el(
      "div",
      { class: "actions" },
      link("Ouvrir l’explorateur", "/network/explorer"),
      button("Actualiser l’activité", refresh, "secondary"),
    ),
    el(
      "section",
      {
        class: "network-paths",
        "aria-label": "Choisir son rôle dans le réseau",
      },
      el(
        "article",
        { class: "card" },
        el("span", { class: "pill positive" }, "Disponible · observateur"),
        el("h2", {}, "Vérifier le réseau"),
        el(
          "p",
          {},
          "Conservez une copie du registre et vérifiez les blocs sur votre ordinateur. Ce rôle consulte et contrôle le réseau ; il ne vote pas au consensus.",
        ),
        el(
          "div",
          { class: "actions" },
          link("Installer un observateur", "/network/observer"),
        ),
      ),
      el(
        "article",
        { class: "card" },
        el("span", { class: "pill" }, "Maquette · à venir"),
        el("h2", {}, "Participer au consensus"),
        el(
          "p",
          {},
          "Découvrez le futur parcours pour contribuer à la validation collective des blocs. Cet aperçu présente une fonctionnalité qui reste à développer.",
        ),
        el(
          "div",
          { class: "actions" },
          link("Voir la maquette du consensus", "/network/consensus"),
        ),
      ),
    ),
    el(
      "section",
      { class: "card" },
      el("h2", {}, "Le réseau actuel"),
      el(
        "dl",
        { class: "facts" },
        ...[
          ["Réseau", client.choice.name],
          ["Identifiant", client.network],
          ["Genèse SHA-256", client.choice.manifest.main.genesis.sha256],
          ["Hauteur vérifiée", client.height],
          ["Version de protocole", client.config.registry.protocol_epoch],
          [
            "Fonctionnement",
            "Atelier local expérimental · transport HTTP pour les observateurs",
          ],
          [
            "Consensus de l’atelier",
            "Commits synthétiques avec clés publiques TEST_ONLY",
          ],
        ].flatMap(([name, value]) => [
          el("dt", {}, name!),
          el("dd", { class: "identifier" }, value!),
        ]),
      ),
    ),
    el(
      "section",
      { class: "card" },
      el("h2", {}, "Quel rôle pour mon ordinateur ?"),
      el(
        "p",
        {},
        "Un nœud observateur télécharge les blocs, vérifie les signatures et rejoue les décisions depuis la genèse. Il conserve une copie locale du registre et reprend après un arrêt. Le droit de valider des blocs ne s’obtient pas en téléchargeant l’archive.",
      ),
      link("Explorer les blocs et les états", "/network/explorer"),
    ),
  );
  await refresh();
  if (generation === viewGeneration)
    networkPoll = setInterval(() => {
      if (!document.hidden) void refresh();
    }, 15000);
}

async function listenNetworkPage() {
  const command = (value: string) =>
    el(
      "div",
      { class: "command" },
      el("pre", {}, el("code", {}, value)),
      button(
        "Copier la commande",
        async () => {
          await navigator.clipboard.writeText(value);
          flash(
            "Commande copiée. Ouvrez un terminal dans le dossier extrait et collez-la.",
          );
        },
        "secondary",
      ),
    );
  const platforms = [
    ["Windows 64 bits", "windows-amd64", ".\\civora-reseau.exe"],
    [
      "Linux 64 bits",
      "linux-amd64",
      "chmod +x civora-reseau && ./civora-reseau",
    ],
  ];
  renderPage(
    generation,
    header(
      "Vérifier le réseau",
      "Un téléchargement. Une commande.",
      "Installez un nœud observateur : votre ordinateur conserve et vérifie une copie du registre actuel. Ce rôle ne participe pas au consensus. Aucun compte civique, aucune installation de Go ou Node.js et aucun port entrant à ouvrir.",
    ),
    !participationAvailable
      ? el(
          "p",
          { class: "notice" },
          "Les archives de participation ne sont pas proposées par cet hôte.",
        )
      : el(
          "ol",
          { class: "join-steps" },
          el(
            "li",
            {},
            el("h2", {}, "Télécharger l’archive de votre ordinateur"),
            el(
              "div",
              { class: "network-downloads" },
              ...platforms.map(([name, target, cmd]) =>
                el(
                  "article",
                  { class: "card" },
                  el("h3", {}, name!),
                  el("p", {}, "Archive autonome, configurée pour ce réseau."),
                  el(
                    "a",
                    {
                      href: "/network/download/" + target + ".zip",
                      class: "primary",
                      download: "",
                    },
                    "Télécharger pour " + name,
                  ),
                  command(cmd!),
                  el(
                    "a",
                    {
                      href: "/network/download/" + target + ".sha256",
                      target: "_blank",
                      rel: "noopener",
                    },
                    "Voir l’empreinte SHA-256",
                  ),
                ),
              ),
            ),
          ),
          el(
            "li",
            {},
            el("h2", {}, "Extraire, puis lancer la commande"),
            el(
              "p",
              {},
              "Extrayez toute l’archive dans un dossier. Ouvrez un terminal dans ce dossier et lancez la commande indiquée pour votre système. Le programme affiche la hauteur du registre vérifié et annonce sa présence au réseau.",
            ),
          ),
          el(
            "li",
            {},
            el("h2", {}, "Laisser le terminal ouvert"),
            el(
              "p",
              {},
              "Le compteur du réseau se met à jour automatiquement. Pour arrêter : Ctrl+C. Pour reprendre : relancez la même commande, dans le même dossier. Conservez le dossier donnees ; il contient votre archive et votre identité locale de nœud.",
            ),
          ),
        ),
    el(
      "section",
      { class: "card" },
      el("h2", {}, "Depuis un autre ordinateur du même Wi-Fi"),
      el(
        "p",
        {},
        "Ouvrez cette page depuis l’adresse HTTPS Wi-Fi du serveur, puis téléchargez l’archive sur cet ordinateur. Une archive téléchargée depuis 127.0.0.1 se connecte uniquement à cette adresse locale.",
      ),
      el(
        "p",
        {},
        "Le certificat public de l’atelier est inclus si nécessaire et reste propre au programme ; aucune autorité n’est installée automatiquement sur votre système. Ne désactivez pas la vérification TLS.",
      ),
    ),
    el(
      "details",
      {},
      el("summary", {}, "Confiance, données et limites de cet atelier"),
      el(
        "p",
        {},
        "Cet atelier est expérimental et utilise des clés de consensus publiques de test. Les observateurs vérifient ses blocs mais ne constituent pas un quorum de validateurs indépendant. Les présences expirent après 45 secondes sans nouvelles et ne représentent pas un nombre de personnes.",
      ),
      el(
        "p",
        {},
        "Comparez l’empreinte de la genèse et celle de l’archive avec l’organisateur par un canal de confiance. Les empreintes fournies sur la même page ne remplacent pas cette vérification.",
      ),
      el(
        "p",
        {},
        "La clé de présence est créée uniquement au premier lancement. Ne partagez pas le dossier donnees. L’archive téléchargée ne contient ni clé privée de compte, ni clé privée du serveur. Si un nouvel atelier remplace ce réseau, téléchargez une nouvelle archive et utilisez un autre dossier ; les données de l’ancien réseau restent conservées.",
      ),
      el(
        "pre",
        {},
        "Genèse SHA-256 : " + client.choice.manifest.main.genesis.sha256,
      ),
    ),
    link("Voir l’activité du réseau", "/network"),
  );
}

function consensusNetworkMockup() {
  renderPage(
    generation,
    header(
      "Maquette · fonctionnalité à venir",
      "Participer au consensus.",
      "Aperçu du futur parcours pour rejoindre le réseau comme validateur et contribuer à la validation collective des blocs.",
    ),
    el(
      "section",
      { class: "network-mock-notice", role: "note" },
      el("strong", {}, "Cette page est une maquette."),
      el(
        "p",
        {},
        "La participation au consensus n’est pas développée pour cet atelier. Aucun téléchargement de validateur, aucune inscription et aucune activation ne sont disponibles ici.",
      ),
    ),
    el(
      "section",
      { class: "card" },
      el("h2", {}, "Un rôle de validation collective"),
      el(
        "p",
        {},
        "Un validateur contribuerait au consensus en proposant des blocs ou en votant sur leur validité. Les conditions d’admission et le parcours d’activation restent à définir.",
      ),
      el(
        "p",
        { class: "muted" },
        "L’observateur disponible aujourd’hui vérifie sa copie locale du registre et n’exerce pas ce rôle.",
      ),
    ),
    el(
      "section",
      { "aria-label": "Aperçu du futur parcours" },
      el("h2", {}, "Le parcours envisagé"),
      el(
        "p",
        { class: "muted" },
        "Étapes indicatives, à préciser lors du développement.",
      ),
      el(
        "ol",
        { class: "join-steps" },
        el(
          "li",
          {},
          el("h3", {}, "Comprendre les conditions du réseau"),
          el(
            "p",
            {},
            "Consulter les responsabilités du rôle et les conditions de participation.",
          ),
        ),
        el(
          "li",
          {},
          el("h3", {}, "Préparer son ordinateur"),
          el(
            "p",
            {},
            "Vérifier les prérequis et préparer la configuration du nœud validateur.",
          ),
        ),
        el(
          "li",
          {},
          el("h3", {}, "Suivre l’admission et l’activation"),
          el(
            "p",
            {},
            "Suivre le parcours qui permettra de participer au consensus une fois le nœud admis.",
          ),
        ),
      ),
    ),
    el(
      "div",
      { class: "actions" },
      el(
        "button",
        {
          type: "button",
          class: "primary",
          disabled: "",
          "aria-describedby": "consensus-unavailable",
        },
        "Participation au consensus indisponible",
      ),
    ),
    el(
      "p",
      { id: "consensus-unavailable", class: "source-note" },
      "Aucune action n’est activée dans cette maquette.",
    ),
    el(
      "section",
      { class: "card" },
      el("h2", {}, "Vous souhaitez utiliser le réseau dès maintenant ?"),
      el(
        "p",
        {},
        "Le mode observateur est disponible : téléchargez son archive pour lire, écouter et vérifier le registre.",
      ),
      link("Vérifier le réseau", "/network/observer"),
    ),
  );
}

function updateNavigation() {
  const technical =
    location.hash.startsWith("#/network") || location.hash === "#/verify";
  document.body.dataset.space = technical ? "network" : "application";
  document.querySelector<HTMLElement>("#application-nav")!.hidden = technical;
  document.querySelector<HTMLElement>("#network-nav")!.hidden = !technical;
  document.querySelector<HTMLElement>(".account-link")!.hidden = technical;
  document.querySelector<HTMLElement>("#workshop-link")!.hidden = !development;
  for (const a of document.querySelectorAll<HTMLAnchorElement>(
    ".space-switch a",
  ))
    a.setAttribute(
      "aria-current",
      (a.dataset.space === "network") === technical ? "page" : "false",
    );
}

async function route() {
  if (!client) return;
  if (networkPoll) {
    clearInterval(networkPoll);
    networkPoll = null;
  }
  updateNavigation();
  routing++;
  updateConnection();
  const current = ++generation;
  cancelWorker();
  const parts = (location.hash.slice(1) || "/")
      .split("?")[0]!
      .split("/")
      .filter(Boolean),
    query = new URLSearchParams(location.hash.split("?")[1] ?? "");
  try {
    const requested = query.get("network");
    if (requested && requested !== client.network) {
      throw Error("ERR_NETWORK");
    }
    await client.refresh();
    account =
      parts[0] === "network" && parts[1] === "explorer"
        ? null
        : await loadAccount(client.network);
    if (current !== generation) return;
    if (!parts.length) await home();
    else if (parts[0] === "account") await accountPage();
    else if (parts[0] === "proposals")
      parts[1] ? await detail(parts[1]) : await proposals();
    else if (parts[0] === "polls" && parts[1]) await detail(parts[1], true);
    else if (parts[0] === "new")
      await newProposal(parts[1]!, query.get("target") ?? undefined);
    else if (parts[0] === "laws" || parts[0] === "roles")
      await objects(parts[0], parts[1]);
    else if (parts[0] === "network") {
      if (["observer", "listen", "join"].includes(parts[1] ?? ""))
        await listenNetworkPage();
      else if (parts[1] === "explorer") {
        if (parts[2] === "state") await explorerStatePage(query);
        else await explorerBlocksPage(query);
      } else if (parts[1] === "consensus") consensusNetworkMockup();
      else if (parts[1] === "receipt" || parts[1] === "verify")
        await verification();
      else if (parts[1] === "workshop")
        await workshopPage(query.get("proposal"));
      else await networkPage();
    } else if (parts[0] === "verify") await verification();
    else if (parts[0] === "news") await newsPage();
    else location.hash = "/";
    const networkTab = ["observer", "listen", "join"].includes(parts[1] ?? "")
      ? "#/network/observer"
      : parts[0] === "verify" || ["receipt", "verify"].includes(parts[1] ?? "")
        ? "#/network/receipt"
        : parts[1] === "explorer"
          ? "#/network/explorer"
          : "#/" + parts.join("/");
    for (const a of document.querySelectorAll<HTMLAnchorElement>(
      "#application-nav a, #network-nav a",
    ))
      a.setAttribute(
        "aria-current",
        parts[0] === "network" || parts[0] === "verify"
          ? a.hash === networkTab
            ? "page"
            : "false"
          : a.hash.split("/")[1] === (parts[0] ?? "")
            ? "page"
            : "false",
      );
  } catch (e) {
    if (current !== generation) return;
    flash(errorMessage(e), true);
    setPage(
      header(
        "Accès au dossier",
        "Cette vue n’est pas disponible.",
        "Vous pouvez revenir à l’accueil ou changer de source.",
      ),
      link("Retour à l’accueil", "/"),
    );
  } finally {
    routing--;
    updateConnection();
  }
}
async function switchNetwork(index: number) {
  generation++;
  cancelWorker();
  account = null;
  client = new Client(choices[index]!);
  connection.textContent = "Vérification du réseau…";
  connection.dataset.state = "pending";
  root.replaceChildren(
    header(
      "Choix du réseau",
      "Vérification de la genèse et de la version…",
      "Les actions restent bloquées pendant la vérification de la source.",
    ),
  );
  try {
    await client.initialize();
    notice.hidden = true;
    await route();
  } catch (e) {
    updateConnection();
    flash(errorMessage(e), true);
    setPage(
      header(
        "Protection du client",
        "Connexion bloquée.",
        "La genèse, la version ou les preuves de cette source ne peuvent pas être validées.",
      ),
    );
  }
}
async function start() {
  let initializing = true,
    navigationPending = false;
  window.addEventListener("hashchange", () => {
    if (initializing) navigationPending = true;
    else void route();
  });
  try {
    const res = await fetch("/pwa-config.json", { cache: "no-cache" }),
      config = await res.json();
    if (
      config.version !== "0.8.0" ||
      !Array.isArray(config.networks) ||
      !config.networks.length
    )
      throw Error("ERR_VERSION");
    choices = config.networks.slice(0, 1);
    participationAvailable = config.participation === true;
    notificationRoot =
      config.notifications?.api_root === "/notifications"
        ? "/notifications"
        : null;
    development = !!config.development;
    await switchNetwork(0);
    initializing = false;
    if (navigationPending) await route();
    document.addEventListener("click", (event) => {
      const a = (event.target as Element).closest<HTMLAnchorElement>(
        'a[href^="#/"]',
      );
      if (a && a.hash === location.hash) {
        event.preventDefault();
        void route();
      }
    });
    window.addEventListener("offline", () => {
      updateConnection();
      flash(
        "Hors ligne : consultation du cache uniquement. Aucun bulletin ne sera envoyé automatiquement.",
      );
    });
    window.addEventListener("online", () => {
      flash("Connexion rétablie. Actualisez le contexte avant toute action.");
      updateConnection();
    });
    document.querySelector("#refresh")!.addEventListener(
      "click",
      () =>
        void task(async () => {
          await client.refresh();
          await route();
        }),
    );
    if ("serviceWorker" in navigator) {
      try {
        const registration = await navigator.serviceWorker.register("/sw.js");
        if (registration.waiting && !notice.classList.contains("error"))
          flash(
            "Une nouvelle version est disponible. Terminez votre opération puis rechargez la page.",
          );
      } catch {
        if (!notice.classList.contains("error"))
          flash(
            "Le mode hors ligne n’a pas pu être activé. Les fonctions en ligne restent accessibles.",
          );
      }
    }
  } catch (e) {
    flash(errorMessage(e), true);
    setPage(
      header(
        "Démarrage",
        "L’application ne peut pas démarrer.",
        "Vérifiez la configuration et les artefacts locaux.",
      ),
    );
  }
}
void start();
