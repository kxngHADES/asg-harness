package agent

import (
	"fmt"
	"strings"
)

// A Domain is the kind of work ASG is doing: software, documentation,
// security, DevOps or project management. It is separate from Mode, which
// says how ASG works (build / plan / loop / ask). Each domain brings a
// Workflow: the phases the model must go through, the rules it must keep,
// and the angles it reviews its own work from.
type Domain string

const (
	DomainSoftware Domain = "software"
	DomainDocs     Domain = "docs"
	DomainCyber    Domain = "cyber"
	DomainDevOps   Domain = "devops"
	DomainPM       Domain = "pm"
)

// Flavor is a sub-mode of a domain, e.g. software/work or cyber/red.
type Flavor struct {
	Name     string
	Describe string
	// Phases is the loop the model must follow, in order.
	Phases []string
	// Rules are hard rules for this flavor.
	Rules []string
	// Lenses are the angles the self-review passes look from, one per pass.
	Lenses []string
	// Finale are loop steps added once all planned steps pass verification
	// (e.g. a security test or the final report).
	Finale []string
}

// Workflow describes a domain and its flavors.
type Workflow struct {
	Domain   Domain
	Label    string
	Describe string
	// Rules apply to every flavor of the domain.
	Rules   []string
	Flavors []Flavor // the first one is the default
}

// Flavor returns the named flavor, or the default one.
func (w *Workflow) Flavor(name string) *Flavor {
	for i := range w.Flavors {
		if w.Flavors[i].Name == name {
			return &w.Flavors[i]
		}
	}
	return &w.Flavors[0]
}

// commonLenses back up a flavor that defines fewer lenses than passes.
var commonLenses = []string{
	"logic — does each step of your reasoning follow from the facts and tool results? Look for wrong assumptions, off-by-one mistakes and contradictions",
	"completeness — compare your answer with the original request line by line. Is anything asked for missing, half done, or done differently than asked?",
	"edge cases and failure modes — empty input, errors, missing files, unexpected sizes or states. What breaks?",
	"evidence — is every claim backed by something you actually read or ran? Remove or verify anything you guessed",
}

// Workflows lists every domain in the order Ctrl+T cycles through them.
var Workflows = []*Workflow{
	{
		Domain:   DomainSoftware,
		Label:    "software",
		Describe: "write, fix and ship code",
		Rules: []string{
			"Read the code you are changing before you change it, and follow the project's existing style.",
		},
		Flavors: []Flavor{
			{
				Name:     "project",
				Describe: "balanced: solid, tested code for your own or a team project",
				Phases: []string{
					"Plan — understand the request and the code it touches.",
					"Build — make the change in small steps, following the project's patterns.",
					"Test — add or update tests and run them.",
					"Document — update the README or docs the change affects.",
					"Verify — build and run the tests; fix failures until they pass.",
				},
				Rules: []string{
					"Keep changes focused on the request; don't refactor unrelated code.",
					"Prefer simple code over clever code.",
				},
				Lenses: []string{
					"scope — does the change do what was asked, without unrelated edits?",
					"tests — do the tests cover the change and did they pass after the last edit?",
				},
			},
			{
				Name:     "work",
				Describe: "strict: only code this build needs, caller first, test → document → verify → security",
				Phases: []string{
					"Plan — restate the exact thing being built and list the pieces it needs. Nothing else goes in.",
					"Caller first — write the code that will CALL a new function before writing the function itself, so the call site decides its shape.",
					"Implement — now write the function the caller needs, exactly as the caller uses it.",
					"Test — write or extend tests for what you built and run them.",
					"Document — update docs, comments and changelog entries that this change affects.",
					"Verify — run the build and the full test suite. If anything fails, read the failure, find the root cause, fix it, and verify again. Loop until everything passes.",
					"Security — check the change for injection, unsafe input handling, secrets, path traversal, permissions and dependency risks; fix what you find and verify again.",
				},
				Rules: []string{
					"Write ONLY code that is definitely used by the exact thing being built right now. Never write code that is not for this exact build.",
					"Never build something before it is needed: no anticipation code — no speculative parameters, options, hooks, interfaces, abstractions, config or 'for later' helpers.",
					"Work top-down: write the caller of a function before the function's body.",
					"A failing test is never ignored, skipped or deleted to get green — find and fix the cause.",
				},
				Lenses: []string{
					"anticipation code — is there any function, parameter, branch, option or file that nothing in this build calls or needs right now? Remove it",
					"caller-first and wiring — is every new function actually called, and does its shape match exactly how the caller uses it?",
					"tests and verification — did the tests really run and pass after your last change? Are the important paths covered? Re-run if unsure",
					"security — injection, unsafe input, secrets in code or logs, path traversal, permissions, risky dependencies",
				},
				Finale: []string{
					"Security test: review every file changed in this run for injection, unsafe input handling, secrets, path traversal and permission problems; fix anything found and re-run the checks.",
				},
			},
			{
				Name:     "hobby",
				Describe: "light: get it working fast, learn as you go",
				Phases: []string{
					"Sketch — decide the quickest path to something that works.",
					"Build — make it work end to end.",
					"Try it — run it once and fix what breaks.",
				},
				Rules: []string{
					"Favour getting something working over polish; explain anything new the user might want to learn.",
				},
			},
		},
	},
	{
		Domain:   DomainDocs,
		Label:    "docs",
		Describe: "write and improve documentation, looking at how it renders",
		Rules: []string{
			"Judge documentation by how it LOOKS when rendered, not only by its source. Use the screenshot tool to see the rendered page (a .md/.html file, a docs URL or the screen) and decide changes from the image.",
			"After each round of edits, take a new screenshot and compare it with the previous one. Repeat until headings, spacing, code blocks, tables, images and links render correctly.",
			"For Word, PowerPoint or Excel deliverables use the office_document tool.",
		},
		Flavors: []Flavor{
			{
				Name:     "update",
				Describe: "screenshot the docs, see what's wrong, fix, re-screenshot",
				Phases: []string{
					"Look — screenshot the documentation as it renders now.",
					"Diagnose — from the image, list what is wrong or missing (structure, clarity, formatting, broken images or links, outdated content).",
					"Edit — fix the source files.",
					"Re-check — screenshot again and compare with the first image. Loop back to Diagnose until it looks right.",
				},
				Lenses: []string{
					"rendering — would every heading, list, table, code block and image render correctly? Check the last screenshot",
					"accuracy — does every command, path, flag and API name in the docs match the actual code?",
				},
				Finale: []string{"Take a final screenshot of every page you changed and confirm each one renders correctly."},
			},
			{
				Name:     "write",
				Describe: "write new documentation from the code up",
				Phases: []string{
					"Research — read the code and existing docs to learn what needs documenting.",
					"Outline — decide the pages and sections.",
					"Write — write the docs with runnable examples.",
					"Render & check — screenshot the rendered docs and fix what looks wrong.",
				},
				Lenses: []string{
					"accuracy — does every command, path, flag and API name match the actual code?",
					"reader — can a newcomer follow it from the top without prior knowledge?",
				},
				Finale: []string{"Take a final screenshot of every new page and confirm each one renders correctly."},
			},
		},
	},
	{
		Domain:   DomainCyber,
		Label:    "cyber",
		Describe: "authorized red team (attack) or blue team (defend) work, ending in a full report",
		Rules: []string{
			"Only work on systems the user is authorized to test. Before any active scanning or exploitation, confirm the scope and authorization with ask_user unless it is already stated.",
			"Stay inside the agreed scope. No denial of service, no destruction of data, no persistence beyond what the engagement allows.",
			"Record every action and finding as you go — the harness also logs every tool call to activity.md in the work folder.",
			"Finish with a report (report.md in the work folder, plus a .docx via office_document if the user wants one) covering scope, method, every action taken, every finding with severity, evidence and remediation.",
		},
		Flavors: []Flavor{
			{
				Name:     "red",
				Describe: "offensive: recon → enumerate → find and validate vulnerabilities → report",
				Phases: []string{
					"Scope — confirm targets, authorization, rules of engagement and what is off-limits.",
					"Recon — gather information about the in-scope targets.",
					"Enumerate — services, versions, endpoints, users, configurations.",
					"Analyse — map findings to known weaknesses and misconfigurations.",
					"Validate — safely prove impact for in-scope findings only, with minimal footprint.",
					"Report — write the report: everything done, everything found, severity, evidence, remediation.",
				},
				Lenses: []string{
					"scope — was every action inside the agreed scope and authorization?",
					"evidence — is every finding backed by output you actually captured, with a reproducible step?",
					"severity — is each severity rating justified by real impact and exploitability?",
					"remediation — does every finding have a concrete, correct fix?",
				},
				Finale: []string{"Write the final engagement report to report.md in the work folder: scope, method, timeline of every action, all findings with severity, evidence and remediation, and an executive summary."},
			},
			{
				Name:     "blue",
				Describe: "defensive: inventory → threat model → detect → harden → respond → report",
				Phases: []string{
					"Inventory — what assets, services, data and access exist.",
					"Threat model — who would attack this, how, and what matters most.",
					"Detect — review logs, configs and code for signs of compromise and weaknesses.",
					"Harden — fix or recommend fixes, most important first.",
					"Respond — if anything suspicious is found, contain it and document it.",
					"Report — write the report: everything checked, everything found, what was changed.",
				},
				Lenses: []string{
					"coverage — which assets or attack paths were not checked?",
					"evidence — is every finding backed by output you actually captured?",
					"priority — are the fixes ordered by real risk?",
					"regressions — could any hardening change break something?",
				},
				Finale: []string{"Write the final defensive assessment report to report.md in the work folder: scope, everything checked, all findings with severity and evidence, changes made, and remaining recommendations."},
			},
		},
	},
	{
		Domain:   DomainDevOps,
		Label:    "devops",
		Describe: "review and fix CI/CD pipelines, builds, containers and infrastructure",
		Rules: []string{
			"Find every pipeline and infra file first: .github/workflows, .gitlab-ci.yml, Jenkinsfile, azure-pipelines.yml, Dockerfile, docker-compose, Kubernetes manifests, Terraform, Makefiles, release configs.",
			"Validate with real tools when they are installed (actionlint, yamllint, hadolint, docker build, terraform validate); say when a check could not be run.",
		},
		Flavors: []Flavor{
			{
				Name:     "review",
				Describe: "audit pipelines: correctness, security, speed, reliability",
				Phases: []string{
					"Inventory — list every pipeline and infra file and what each one does.",
					"Correctness — triggers, job order, matrix, caching, artifacts, versions.",
					"Security — secrets handling, token permissions, actions pinned by SHA, untrusted input in scripts, supply chain.",
					"Speed & reliability — caching, parallelism, flaky steps, timeouts, retries.",
					"Report — findings by severity, each with the exact fix.",
				},
				Lenses: []string{
					"security — least-privilege permissions, secret exposure, unpinned third-party actions, script injection",
					"correctness — would each job actually run and pass on the events it claims to handle?",
				},
				Finale: []string{"Write the pipeline review report to report.md in the work folder: every file reviewed, findings by severity with exact fixes."},
			},
			{
				Name:     "pipeline",
				Describe: "write or fix pipelines and validate them",
				Phases: []string{
					"Understand — how the project builds, tests and ships.",
					"Change — write or fix the pipeline.",
					"Validate — lint and dry-run what you can locally.",
					"Harden — permissions, pinning, secrets, caching.",
				},
				Lenses: []string{
					"security — least-privilege permissions, secret exposure, unpinned third-party actions, script injection",
					"correctness — would each job actually run and pass on the events it claims to handle?",
				},
			},
		},
	},
	{
		Domain:   DomainPM,
		Label:    "pm",
		Describe: "project management: plans, sprints, risks, status — reviewed at least 4 times",
		Rules: []string{
			"Base plans on facts: read the repo, issues, docs and notes that exist. Ask the user (ask_user) for deadlines, team size and priorities you cannot find.",
			"Write deliverables to the work folder (and as .docx / .pptx / .xlsx via office_document when the user wants files).",
		},
		Flavors: []Flavor{
			{
				Name:     "plan",
				Describe: "project plan: scope, work breakdown, milestones, estimates, risks",
				Phases: []string{
					"Scope — goals, deliverables, what is out of scope, success criteria.",
					"Breakdown — work breakdown structure down to tasks of a few days at most.",
					"Sequence — dependencies, critical path and milestones.",
					"Estimate — effort ranges (best / likely / worst) and capacity.",
					"Risks — risk register with likelihood, impact, owner and mitigation.",
					"Communicate — stakeholders, RACI, cadence.",
				},
				Lenses: []string{
					"dependencies and order — can every task actually start when the plan says? Is the critical path right?",
					"estimates and capacity — are estimates realistic for the team, with buffer? Does the timeline add up?",
					"risks and assumptions — what unstated assumption could sink this? Is every major risk owned and mitigated?",
					"scope and acceptance — does every deliverable have clear acceptance criteria? Is anything in scope missing a task?",
				},
			},
			{
				Name:     "sprint",
				Describe: "sprint planning: backlog, capacity, sprint goal, tickets",
				Phases: []string{
					"Backlog — gather and clarify the candidate work.",
					"Capacity — who is available and how much.",
					"Select — pick work that fits capacity toward one sprint goal.",
					"Tickets — write each item with acceptance criteria and estimate.",
				},
				Lenses: []string{
					"capacity — does the selected work fit the real capacity with buffer?",
					"acceptance — does every ticket have testable acceptance criteria?",
					"dependencies — is anything blocked by work outside the sprint?",
					"goal — does every ticket serve the sprint goal?",
				},
			},
			{
				Name:     "status",
				Describe: "status report: progress, risks, decisions needed",
				Phases: []string{
					"Gather — what changed since the last report (commits, issues, notes).",
					"Assess — on track / at risk / off track, with reasons.",
					"Report — progress, next steps, risks, decisions needed.",
				},
				Lenses: []string{
					"accuracy — is every status claim backed by real data?",
					"risks — is anything at risk that the report glosses over?",
				},
			},
		},
	},
}

// WorkflowFor returns the workflow for a domain (software when unknown).
func WorkflowFor(d Domain) *Workflow {
	for _, w := range Workflows {
		if w.Domain == d {
			return w
		}
	}
	return Workflows[0]
}

// ParseDomain accepts a domain name or alias.
func ParseDomain(s string) (Domain, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "software", "sw", "code", "dev":
		return DomainSoftware, true
	case "docs", "doc", "documentation":
		return DomainDocs, true
	case "cyber", "security", "sec":
		return DomainCyber, true
	case "devops", "ops", "ci", "cicd":
		return DomainDevOps, true
	case "pm", "project-manager", "projectmanager", "manager":
		return DomainPM, true
	}
	return "", false
}

// NextDomain returns the domain after d in the Ctrl+T cycle.
func NextDomain(d Domain) Domain {
	for i, w := range Workflows {
		if w.Domain == d {
			return Workflows[(i+1)%len(Workflows)].Domain
		}
	}
	return DomainSoftware
}

// Track is the domain and flavor a session works in.
type Track struct {
	Domain Domain `json:"domain"`
	Flavor string `json:"flavor"`
}

func (t Track) Workflow() *Workflow { return WorkflowFor(t.Domain) }

// FlavorSpec returns the active flavor.
func (t Track) FlavorSpec() *Flavor { return t.Workflow().Flavor(t.Flavor) }

// String is e.g. "software/work".
func (t Track) String() string {
	return string(t.Workflow().Domain) + "/" + t.FlavorSpec().Name
}

// WithFlavor returns the track with a flavor, if the domain has it.
func (t Track) WithFlavor(name string) (Track, bool) {
	for _, f := range t.Workflow().Flavors {
		if strings.EqualFold(f.Name, name) {
			t.Flavor = f.Name
			return t, true
		}
	}
	return t, false
}

// Lenses returns n review lenses: the flavor's own first, then common ones.
func (t Track) Lenses(n int) []string {
	all := append(append([]string{}, t.FlavorSpec().Lenses...), commonLenses...)
	var out []string
	for i := 0; i < n; i++ {
		out = append(out, all[i%len(all)])
	}
	return out
}

// instructions is the system prompt section for the track.
func (t Track) instructions() string {
	w, f := t.Workflow(), t.FlavorSpec()
	var b strings.Builder
	fmt.Fprintf(&b, "\n\n## Domain: %s — %s (%s)\n", strings.ToUpper(w.Label), f.Name, f.Describe)
	if len(f.Phases) > 0 {
		b.WriteString("Follow this loop, in order. Say which phase you are in as you go; go back to an earlier phase whenever a later one fails:\n")
		for i, p := range f.Phases {
			fmt.Fprintf(&b, "%d. %s\n", i+1, p)
		}
	}
	rules := append(append([]string{}, w.Rules...), f.Rules...)
	if len(rules) > 0 {
		b.WriteString("Rules:\n")
		for _, r := range rules {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}
	return b.String()
}
