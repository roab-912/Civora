package governance

import (
	"civora/internal/codec"
	"civora/internal/ports"
	"encoding/json"
	"fmt"
	"golang.org/x/text/language"
	"sort"
	"strings"
	"unicode/utf8"
)

func terminal(status string) bool {
	switch status {
	case "applied", "rejected", "conflict", "expired", "failed_setup", "failed_window", "failed_tally":
		return true
	}
	return false
}
func (s *State) Submit(x Execution, id, author string, raw []byte) error {
	var req Submission
	if e := json.Unmarshal(raw, &req); e != nil {
		return e
	}
	if strings.TrimSpace(req.Rationale) == "" || len(req.Rationale) > 16*1024 {
		return fmt.Errorf("ERR_RATIONALE")
	}
	if _, ok := s.Proposals[id]; ok {
		return fmt.Errorf("ERR_DUPLICATE_PROPOSAL")
	}
	active := 0
	for _, p := range s.Proposals {
		if !terminal(p.State) {
			active++
		}
	}
	if active >= 128 {
		return fmt.Errorf("ERR_CAPACITY")
	}
	p := &Proposal{Submission: req, Author: author, State: "collecting", Policy: s.Network.Policy, PolicyRevision: s.Network.PolicyRevision, Guardians: append([]Guardian{}, s.Network.Guardians...), GuardianEpoch: s.Network.GuardianEpoch, Endorsements: []string{}, SubmittedHeight: x.Height, SubmittedMS: str(x.Time)}
	for i, g := range p.Guardians {
		a, ok := x.Accounts[g.AccountID]
		if !ok || !a.Active {
			return fmt.Errorf("ERR_GUARDIANS")
		}
		p.Guardians[i].SigningPK = a.SigningPK
	}
	if e := s.preconditions(p, x.Time, x.Accounts); e != nil {
		return e
	}
	deadline, e := add(x.Time, num(p.Policy.CollectionMS))
	if e != nil {
		return e
	}
	p.CollectionDueMS = str(deadline)
	s.Proposals[id] = p
	return nil
}
func (s *State) Endorse(x Execution, id, actor string) error {
	p, ok := s.Proposals[id]
	if !ok || p.State != "collecting" {
		return fmt.Errorf("ERR_PHASE")
	}
	if p.Author == actor {
		return fmt.Errorf("ERR_AUTHOR_ENDORSEMENT")
	}
	for _, a := range p.Endorsements {
		if a == actor {
			return fmt.Errorf("ERR_DUPLICATE_ENDORSEMENT")
		}
	}
	p.Endorsements = append(p.Endorsements, actor)
	sort.Strings(p.Endorsements)
	return nil
}
func (s *State) Qualify(x Execution) error {
	ids := []string{}
	for id, p := range s.Proposals {
		if p.State == "collecting" && uint64(len(p.Endorsements)) >= num(p.Policy.EndorsementsRequired) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		p := s.Proposals[id]
		due, e := add(x.Time, num(p.Policy.QueueMS))
		if e != nil {
			return e
		}
		p.State = "qualified"
		p.QualifiedHeight = ptr(x.Height)
		p.QualifiedMS = ptr(str(x.Time))
		p.QueueDueMS = ptr(str(due))
		x.Emit("proposal.qualified", id)
	}
	return nil
}

func (s *State) preconditions(p *Proposal, effectTime uint64, accounts map[string]AccountView) error {
	parts := strings.Split(p.Action, ".")
	if len(parts) != 2 {
		return fmt.Errorf("ERR_ACTION")
	}
	kind, verb := parts[0], parts[1]
	if kind == "policy" {
		if verb != "update" || p.TargetID != nil || p.ExpectedRevision == nil || *p.ExpectedRevision != s.Network.PolicyRevision {
			return fmt.Errorf("ERR_REVISION")
		}
		var c struct {
			Policy Policy `json:"policy"`
		}
		if e := json.Unmarshal(p.Changes, &c); e != nil {
			return e
		}
		return c.Policy.Validate()
	}
	if kind != "law" && kind != "role" {
		return fmt.Errorf("ERR_ACTION")
	}
	var obj Object
	if verb == "create" {
		if p.TargetID != nil || p.ExpectedRevision != nil {
			return fmt.Errorf("ERR_TARGET")
		}
	} else {
		if p.TargetID == nil || p.ExpectedRevision == nil {
			return fmt.Errorf("ERR_TARGET")
		}
		var ok bool
		obj, ok = s.Objects[tuple(kind, *p.TargetID)]
		if !ok {
			return fmt.Errorf("ERR_OBJECT")
		}
		if obj.Revision != *p.ExpectedRevision {
			return fmt.Errorf("ERR_REVISION")
		}
		if verb != "restore" && obj.Status != "active" {
			return fmt.Errorf("ERR_OBJECT_INACTIVE")
		}
	}
	switch verb {
	case "create", "amend":
		if kind == "law" {
			var c LawContent
			if e := json.Unmarshal(p.Changes, &c); e != nil {
				return e
			}
			if strings.TrimSpace(c.Title) == "" || utf8.RuneCountInString(c.Title) > 160 || len(c.Text) > 128*1024 || c.Text == "" {
				return fmt.Errorf("ERR_TEXT_SIZE")
			}
			if strings.Contains(c.Language, "_") {
				return fmt.Errorf("ERR_LANGUAGE")
			}
			if _, e := language.Parse(c.Language); e != nil {
				return fmt.Errorf("ERR_LANGUAGE")
			}
		} else {
			var c RoleContent
			if e := json.Unmarshal(p.Changes, &c); e != nil {
				return e
			}
			return s.checkRole(c)
		}
	case "repeal":
		if kind != "law" {
			return fmt.Errorf("ERR_ACTION")
		}
	case "retire":
		if kind != "role" {
			return fmt.Errorf("ERR_ACTION")
		}
	case "restore":
		var c struct {
			Source string `json:"source_revision"`
		}
		if e := json.Unmarshal(p.Changes, &c); e != nil {
			return e
		}
		r, ok := s.Revisions[tuple(kind, *p.TargetID, c.Source)]
		if !ok || r.Status != "active" {
			return fmt.Errorf("ERR_SOURCE_REVISION")
		}
		if kind == "role" {
			var content RoleContent
			_ = json.Unmarshal(r.Content, &content)
			return s.checkRole(content)
		}
	case "grant":
		if kind != "role" {
			return fmt.Errorf("ERR_ACTION")
		}
		var c struct {
			AccountID string `json:"account_id"`
			UntilMS   string `json:"until_ms"`
		}
		if e := json.Unmarshal(p.Changes, &c); e != nil {
			return e
		}
		a, ok := accounts[c.AccountID]
		if !ok || !a.Active {
			return fmt.Errorf("ERR_ACCOUNT")
		}
		until := num(c.UntilMS)
		if until <= effectTime || p.State == "adopted" && until-effectTime > 365*Day {
			return fmt.Errorf("ERR_GRANT_PERIOD")
		}
		for _, g := range s.Grants {
			if g.RoleID == *p.TargetID && g.AccountID == c.AccountID && g.Status == "active" && num(g.UntilMS) > effectTime {
				return fmt.Errorf("ERR_GRANT_EXISTS")
			}
		}
	case "revoke":
		if kind != "role" {
			return fmt.Errorf("ERR_ACTION")
		}
		var c struct {
			GrantID string `json:"grant_id"`
		}
		_ = json.Unmarshal(p.Changes, &c)
		g, ok := s.Grants[tuple(*p.TargetID, c.GrantID)]
		if !ok || g.Status != "active" || num(g.UntilMS) <= effectTime {
			return fmt.Errorf("ERR_GRANT")
		}
	default:
		return fmt.Errorf("ERR_ACTION")
	}
	return nil
}
func (s *State) checkRole(c RoleContent) error {
	if strings.TrimSpace(c.Label) == "" || utf8.RuneCountInString(c.Label) > 160 || len(c.Description) > 8*1024 || len(c.Capabilities) > 5 {
		return fmt.Errorf("ERR_ROLE_CONTENT")
	}
	caps := map[string]bool{}
	for _, cap := range c.Capabilities {
		if caps[cap] || cap != "annotate_proposal" && cap != "publish_notice" && cap != "report_implementation" {
			return fmt.Errorf("ERR_CAPABILITY")
		}
		caps[cap] = true
	}
	if c.ScopeID != nil {
		law, ok := s.Objects[tuple("law", *c.ScopeID)]
		if !ok || law.Status != "active" {
			return fmt.Errorf("ERR_SCOPE")
		}
	}
	return nil
}

func (s *State) applyEffect(x Execution, id string, p *Proposal) error {
	if e := s.preconditions(p, x.Time, x.Accounts); e != nil {
		return e
	}
	if x.Effects == nil {
		return fmt.Errorf("ERR_EFFECT_ADAPTER")
	}
	receipt, e := x.Effects.Evaluate(ports.EffectRequest{NetworkID: x.NetworkID, ProposalID: id, Action: p.Action, EffectiveMS: str(x.Time), Payload: codec.Must(p.Submission)})
	if e != nil {
		return e
	}
	if receipt.Profile != "ledger_only_v1" || len(receipt.Reference) != 64 {
		return fmt.Errorf("ERR_EFFECT_RECEIPT")
	}
	kind, verb, _ := strings.Cut(p.Action, ".")
	if kind == "policy" {
		next, e := add(num(s.Network.PolicyRevision), 1)
		if e != nil {
			return e
		}
		var c struct {
			Policy Policy `json:"policy"`
		}
		_ = json.Unmarshal(p.Changes, &c)
		s.Network.Policy = c.Policy
		s.Network.PolicyRevision = str(next)
		p.EffectReceipt = &receipt
		return nil
	}
	idObject := val(p.TargetID)
	if verb == "create" {
		idObject = codec.Hash("object", []any{kind, id})
	}
	if verb == "grant" {
		var c struct {
			AccountID string `json:"account_id"`
			UntilMS   string `json:"until_ms"`
		}
		_ = json.Unmarshal(p.Changes, &c)
		s.Grants[tuple(idObject, id)] = &Grant{AccountID: c.AccountID, RoleID: idObject, FromMS: str(x.Time), UntilMS: c.UntilMS, Status: "active"}
		p.EffectReceipt = &receipt
		return nil
	}
	if verb == "revoke" {
		var c struct {
			GrantID string `json:"grant_id"`
		}
		_ = json.Unmarshal(p.Changes, &c)
		g := s.Grants[tuple(idObject, c.GrantID)]
		g.Status = "revoked"
		g.RevokedBy = ptr(id)
		g.EndedAtMS = ptr(str(x.Time))
		x.Emit("grant.revoked", c.GrantID)
		p.EffectReceipt = &receipt
		return nil
	}
	revision := "1"
	var previous Revision
	if verb != "create" {
		obj := s.Objects[tuple(kind, idObject)]
		n, e := add(num(obj.Revision), 1)
		if e != nil {
			return e
		}
		revision = str(n)
		previous = s.Revisions[tuple(kind, idObject, obj.Revision)]
	}
	r := Revision{Content: append(json.RawMessage{}, p.Changes...), ProposalID: id, Status: "active", EffectiveAtMS: str(x.Time)}
	switch verb {
	case "restore":
		var c struct {
			Source string `json:"source_revision"`
		}
		_ = json.Unmarshal(p.Changes, &c)
		source := s.Revisions[tuple(kind, idObject, c.Source)]
		r.Content = append(json.RawMessage{}, source.Content...)
		r.SourceRevision = ptr(c.Source)
	case "repeal":
		r.Content = previous.Content
		r.Status = "repealed"
	case "retire":
		r.Content = previous.Content
		r.Status = "retired"
		keys := []string{}
		for key, g := range s.Grants {
			if g.RoleID == idObject && g.Status == "active" {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			g := s.Grants[key]
			g.Status = "revoked"
			g.RevokedBy = ptr(id)
			g.EndedAtMS = ptr(str(x.Time))
			var parts []string
			_ = json.Unmarshal([]byte(key), &parts)
			x.Emit("grant.revoked", parts[1])
		}
	}
	s.Objects[tuple(kind, idObject)] = Object{kind, revision, r.Status}
	s.Revisions[tuple(kind, idObject, revision)] = r
	p.EffectReceipt = &receipt
	return nil
}

func (s *State) Publish(x Execution, actor string, raw []byte) error {
	var p Publication
	if e := json.Unmarshal(raw, &p); e != nil {
		return e
	}
	if strings.TrimSpace(p.Content) == "" || len(p.Content) > 8*1024 {
		return fmt.Errorf("ERR_PUBLICATION_SIZE")
	}
	obj, ok := s.Objects[tuple("role", p.RoleID)]
	if !ok || obj.Status != "active" {
		return fmt.Errorf("ERR_ROLE")
	}
	var role RoleContent
	_ = json.Unmarshal(s.Revisions[tuple("role", p.RoleID, obj.Revision)].Content, &role)
	capable := false
	for _, c := range role.Capabilities {
		if c == p.Capability {
			capable = true
		}
	}
	if !capable {
		return fmt.Errorf("ERR_CAPABILITY")
	}
	granted := false
	for _, g := range s.Grants {
		if g.AccountID == actor && g.RoleID == p.RoleID && g.Status == "active" && num(g.FromMS) <= x.Time && x.Time < num(g.UntilMS) {
			granted = true
		}
	}
	if !granted {
		return fmt.Errorf("ERR_GRANT")
	}
	if p.Capability == "annotate_proposal" {
		target, ok := s.Proposals[p.TargetID]
		if !ok {
			return fmt.Errorf("ERR_TARGET")
		}
		if role.ScopeID != nil {
			if !strings.HasPrefix(target.Action, "law.") || target.TargetID == nil || *target.TargetID != *role.ScopeID {
				return fmt.Errorf("ERR_SCOPE")
			}
		}
	} else {
		target, ok := s.Objects[tuple("law", p.TargetID)]
		if !ok || target.Status != "active" {
			return fmt.Errorf("ERR_TARGET")
		}
		if role.ScopeID != nil && *role.ScopeID != p.TargetID {
			return fmt.Errorf("ERR_SCOPE")
		}
	}
	return nil
}
