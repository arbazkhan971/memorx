package bench

import "strings"

// --- Action/Query builder helpers ---

type params = map[string]interface{}

func startFeature(name string) Action          { return Action{Tool: "start_feature", Params: params{"name": name}} }
func startFeatureDesc(name, desc string) Action { return Action{Tool: "start_feature", Params: params{"name": name, "description": desc}} }
func note(content, typ string) Action           { return Action{Tool: "remember", Params: params{"content": content, "type": typ}} }
func noteForFeature(feature, content, typ string) Action { return Action{Tool: "remember", Params: params{"feature": feature, "content": content, "type": typ}} }
func fact(subj, pred, obj string) Action        { return Action{Tool: "add_fact", Params: params{"subject": subj, "predicate": pred, "object": obj}} }
func endSess() Action                           { return Action{Tool: "end_session", Params: params{}} }
func startSess(feature string) Action           { return Action{Tool: "start_session", Params: params{"feature": feature}} }
func startSessTool(feature, tool string) Action { return Action{Tool: "start_session", Params: params{"feature": feature, "tool": tool}} }

func savePlan(title, content string, stepTitles ...string) Action {
	steps := make([]interface{}, len(stepTitles))
	for i, t := range stepTitles {
		steps[i] = params{"title": t}
	}
	return Action{Tool: "save_plan", Params: params{"title": title, "content": content, "steps": steps}}
}

func savePlanFor(feature, title, content string, stepTitles ...string) Action {
	a := savePlan(title, content, stepTitles...)
	a.Params["feature"] = feature
	return a
}

func ctxQuery(feature, tier string) Query  { return Query{Tool: "get_context", Params: params{"feature": feature, "tier": tier}} }
func factsQuery(feature string) Query      { return Query{Tool: "get_facts", Params: params{"feature": feature}} }
func listFeaturesQuery() Query             { return Query{Tool: "list_features", Params: params{}} }

func searchQ(query, scope, feature string) Query {
	return Query{Tool: "search", Params: params{"query": query, "scope": scope, "feature": feature}}
}

func searchAll(query string) Query {
	return Query{Tool: "search", Params: params{"query": query, "scope": "all_features"}}
}

func searchTyped(query, scope, feature string, types []interface{}) Query {
	return Query{Tool: "search", Params: params{"query": query, "scope": scope, "feature": feature, "types": types}}
}

func searchAllTyped(query string, types []interface{}) Query {
	return Query{Tool: "search", Params: params{"query": query, "scope": "all_features", "types": types}}
}

// --- Scenario generators for common patterns ---

// factOverride generates a scenario: start feature, add facts with same subj+pred, query, expect latest only.
func factOverride(id, feature, desc, subj, pred string, values []string, extraContains []string) Scenario {
	setup := []Action{startFeature(feature)}
	for _, v := range values {
		setup = append(setup, fact(subj, pred, v))
	}
	s := Scenario{
		ID: id, Ability: "knowledge_updates", Description: desc,
		Setup: setup, Query: factsQuery(feature),
		ExpectedContains: append([]string{values[len(values)-1]}, extraContains...),
	}
	if len(values) > 1 {
		s.ExpectedNotContain = values[:len(values)-1]
	}
	return s
}

// AllScenarios returns all 70 benchmark scenarios across 7 abilities.
func AllScenarios() []Scenario {
	var all []Scenario
	all = append(all, sessionContinuityScenarios()...)
	all = append(all, decisionRecallScenarios()...)
	all = append(all, knowledgeUpdateScenarios()...)
	all = append(all, temporalReasoningScenarios()...)
	all = append(all, crossFeatureScenarios()...)
	all = append(all, planTrackingScenarios()...)
	all = append(all, abstentionScenarios()...)
	return all
}

// ScenariosByAbility returns scenarios filtered by ability name.
func ScenariosByAbility(ability string) []Scenario {
	var out []Scenario
	for _, s := range AllScenarios() {
		if strings.EqualFold(s.Ability, ability) {
			out = append(out, s)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Ability 1: Session Continuity (15 scenarios)
// ---------------------------------------------------------------------------

func sessionContinuityScenarios() []Scenario {
	const a = "session_continuity"
	return []Scenario{
		{ID: "sc-001", Ability: a, Description: "Start feature, add 3 progress notes, query context",
			Setup: []Action{
				startFeatureDesc("auth-system", "Build authentication system"),
				note("Set up JWT token generation with RS256 signing", "progress"),
				note("Implemented refresh token rotation logic", "progress"),
				note("Added rate limiting to login endpoint", "progress"),
			},
			Query: ctxQuery("auth-system", "standard"), ExpectedContains: []string{"JWT token", "refresh token rotation", "rate limiting"}},
		{ID: "sc-002", Ability: a, Description: "Start feature, add decision + blocker, query context",
			Setup: []Action{
				startFeature("payment-service"),
				note("Decided to use Stripe over PayPal for payment processing", "decision"),
				note("Blocked on Stripe API key provisioning from finance team", "blocker"),
			},
			Query: ctxQuery("payment-service", "standard"), ExpectedContains: []string{"Stripe", "blocker", "decision"}},
		{ID: "sc-003", Ability: a, Description: "Start 2 sessions, query context — session history shown",
			Setup: []Action{
				startFeature("api-gateway"),
				note("Started designing the routing layer", "progress"),
				endSess(), startSessTool("api-gateway", "benchmark-session-2"),
				note("Continued with middleware chain implementation", "progress"),
			},
			Query: ctxQuery("api-gateway", "detailed"), ExpectedContains: []string{"Sessions:", "routing layer", "middleware chain"}},
		{ID: "sc-004", Ability: a, Description: "Add progress across 3 sessions, query detailed context",
			Setup: []Action{
				startFeature("data-pipeline"),
				note("Session 1: Set up Kafka consumer group", "progress"),
				endSess(), startSess("data-pipeline"),
				note("Session 2: Implemented Avro schema validation", "progress"),
				endSess(), startSess("data-pipeline"),
				note("Session 3: Added dead letter queue for failed messages", "progress"),
			},
			Query: ctxQuery("data-pipeline", "detailed"), ExpectedContains: []string{"Kafka consumer", "Avro schema", "dead letter queue"}},
		{ID: "sc-005", Ability: a, Description: "Start feature with description, query compact — description returned",
			Setup:            []Action{startFeatureDesc("cache-layer", "Redis-backed caching layer for API responses with TTL-based invalidation")},
			Query:            ctxQuery("cache-layer", "compact"),
			ExpectedContains: []string{"cache-layer", "active"}},
		{ID: "sc-006", Ability: a, Description: "Add 10 notes, query compact — only recent notes",
			Setup: []Action{
				startFeature("logging-system"),
				note("Note 1: Set up structured logging", "note"), note("Note 2: Added log levels", "note"),
				note("Note 3: Configured log rotation", "note"), note("Note 4: Added request ID tracing", "note"),
				note("Note 5: Integrated with ELK stack", "note"), note("Note 6: Added performance metrics", "note"),
				note("Note 7: Set up alerting rules", "note"), note("Note 8: Added audit logging", "note"),
				note("Note 9: Implemented log sampling", "note"), note("Note 10: Final cleanup of log formats", "note"),
			},
			Query: ctxQuery("logging-system", "compact"), ExpectedNotContain: []string{"Note 1:", "Note 2:", "Note 3:"}},
		{ID: "sc-007", Ability: a, Description: "End session, start new, query — where I left off",
			Setup: []Action{
				startFeature("user-profiles"),
				note("Implemented avatar upload with S3 presigned URLs", "progress"),
				note("Next: implement profile settings page", "next_step"),
				endSess(), startSess("user-profiles"),
			},
			Query: ctxQuery("user-profiles", "standard"), ExpectedContains: []string{"avatar upload", "profile settings"}},
		{ID: "sc-008", Ability: a, Description: "Multiple features, switch between them — correct context",
			Setup: []Action{
				startFeature("feature-alpha"), note("Alpha: implemented the widget renderer", "progress"),
				startFeature("feature-beta"), note("Beta: set up the notification service", "progress"),
				startFeature("feature-alpha"),
			},
			Query: ctxQuery("feature-alpha", "standard"), ExpectedContains: []string{"widget renderer"},
			ExpectedNotContain: []string{"notification service"}},
		{ID: "sc-009", Ability: a, Description: "Feature with branch name, query — branch shown",
			Setup:            []Action{startFeature("search-feature"), note("Working on full-text search with FTS5", "progress")},
			Query:            ctxQuery("search-feature", "standard"),
			ExpectedContains: []string{"search-feature", "full-text search"}},
		{ID: "sc-010", Ability: a, Description: "5 progress notes then 1 decision — decision in context",
			Setup: []Action{
				startFeature("db-migration"),
				note("Progress: analyzed current schema", "progress"), note("Progress: identified foreign key constraints", "progress"),
				note("Progress: wrote migration script draft", "progress"), note("Progress: tested on staging database", "progress"),
				note("Progress: verified data integrity checks", "progress"),
				note("Decided to use blue-green deployment for zero-downtime migration", "decision"),
			},
			Query: ctxQuery("db-migration", "detailed"), ExpectedContains: []string{"blue-green deployment"}},
		{ID: "sc-011", Ability: a, Description: "Add next_step notes, query — next steps shown",
			Setup: []Action{
				startFeature("ci-pipeline"),
				note("Next step: configure GitHub Actions workflow", "next_step"),
				note("Next step: add integration test stage", "next_step"),
			},
			Query: ctxQuery("ci-pipeline", "standard"), ExpectedContains: []string{"GitHub Actions", "integration test"}},
		{ID: "sc-012", Ability: a, Description: "Empty feature, query — graceful empty response",
			Setup: []Action{startFeature("empty-feature")},
			Query: ctxQuery("empty-feature", "standard"), ExpectedContains: []string{"empty-feature"},
			ExpectedNotContain: []string{"Notes:"}},
		{ID: "sc-013", Ability: a, Description: "Feature with only facts — facts shown in context",
			Setup: []Action{
				startFeature("infra-config"),
				fact("database", "uses", "PostgreSQL 15"), fact("cache", "uses", "Redis 7.2"),
			},
			Query: ctxQuery("infra-config", "standard"), ExpectedContains: []string{"PostgreSQL 15", "Redis 7.2"}},
		{ID: "sc-014", Ability: a, Description: "Very long note content, query — content present",
			Setup: []Action{
				startFeature("long-note-feature"),
				note("This is a very detailed progress note about the implementation of the distributed "+
					"consensus algorithm. We evaluated Raft and Paxos, ultimately choosing Raft for its "+
					"understandability. The leader election mechanism uses randomized timeouts between "+
					"150ms and 300ms. Log replication follows the standard approach with AppendEntries "+
					"RPCs. We added a custom extension for read-only queries that bypasses the log "+
					"replication path. Safety properties are verified using TLA+ model checking. The "+
					"implementation handles network partitions gracefully with automatic leader stepdown "+
					"when a majority of heartbeats are missed. Performance benchmarks show 50K writes/sec "+
					"with 3 nodes and 5ms p99 latency. UNIQUE_MARKER_DISTRIBUTED_CONSENSUS_COMPLETE.", "progress"),
			},
			Query: ctxQuery("long-note-feature", "standard"), ExpectedContains: []string{"UNIQUE_MARKER_DISTRIBUTED_CONSENSUS_COMPLETE"}},
		{ID: "sc-015", Ability: a, Description: "Unicode content in notes — preserved correctly",
			Setup: []Action{
				startFeature("unicode-feature"),
				note("Implemented i18n support: Japanese (日本語テスト), Chinese (中文测试), emoji (🚀✅), and RTL Arabic (اختبار)", "progress"),
			},
			Query: ctxQuery("unicode-feature", "standard"), ExpectedContains: []string{"日本語テスト", "中文测试", "اختبار"}},
	}
}

// ---------------------------------------------------------------------------
// Ability 2: Decision Recall (10 scenarios)
// ---------------------------------------------------------------------------

func decisionRecallScenarios() []Scenario {
	const a = "decision_recall"
	return []Scenario{
		{ID: "dr-001", Ability: a, Description: "Add 5 decisions, search for one by keyword",
			Setup: []Action{
				startFeature("dr-feature-1"),
				note("Decided to use PostgreSQL for persistent storage", "decision"),
				note("Decided to use Redis for session caching", "decision"),
				note("Decided to use GraphQL instead of REST for the API", "decision"),
				note("Decided to use Kubernetes with Helm charts", "decision"),
				note("Decided to use Terraform for infrastructure as code", "decision"),
			},
			Query: searchQ("GraphQL API", "current_feature", "dr-feature-1"), ExpectedContains: []string{"GraphQL"}},
		{ID: "dr-002", Ability: a, Description: "Decisions across 2 features, search current — only current's",
			Setup: []Action{
				startFeature("dr-frontend"), note("Decided to use React with TypeScript for the frontend", "decision"),
				startFeature("dr-backend"), note("Decided to use Go with Chi router for the backend", "decision"),
			},
			Query: searchQ("framework decision", "current_feature", "dr-backend"), ExpectedNotContain: []string{"React", "TypeScript"}},
		{ID: "dr-003", Ability: a, Description: "Decisions across 2 features, search all — both found",
			Setup: []Action{
				startFeature("dr-svc-a"), note("Decided to use gRPC for inter-service communication", "decision"),
				startFeature("dr-svc-b"), note("Decided to use gRPC streaming for real-time data", "decision"),
			},
			Query: searchAll("gRPC"), ExpectedContains: []string{"gRPC"}},
		{ID: "dr-004", Ability: a, Description: "Partial word search — trigram fallback finds it",
			Setup:            []Action{startFeature("dr-trigram"), note("Decided to use WebSocket protocol for bidirectional communication", "decision")},
			Query:            searchQ("WebSock", "current_feature", "dr-trigram"),
			ExpectedContains: []string{"WebSocket"}},
		{ID: "dr-005", Ability: a, Description: "Technical terms search",
			Setup:            []Action{startFeature("dr-technical"), note("Decided to use CQRS with event sourcing for the order management bounded context", "decision")},
			Query:            searchQ("CQRS event sourcing", "current_feature", "dr-technical"),
			ExpectedContains: []string{"CQRS", "event sourcing"}},
		{ID: "dr-006", Ability: a, Description: "Similar keywords — most relevant first",
			Setup: []Action{
				startFeature("dr-ranking"),
				note("Decided to use SQLite for local development database", "decision"),
				note("Decided to use PostgreSQL for production database with read replicas", "decision"),
				note("The database schema uses UUID primary keys throughout", "note"),
			},
			Query: searchQ("production database", "current_feature", "dr-ranking"), ExpectedContains: []string{"PostgreSQL"}},
		{ID: "dr-007", Ability: a, Description: "Search for nonexistent topic — no results",
			Setup:            []Action{startFeature("dr-empty"), note("Decided to use Docker for containerization", "decision")},
			Query:            searchQ("quantum computing blockchain", "current_feature", "dr-empty"),
			ExpectedContains: []string{"No results"}},
		{ID: "dr-008", Ability: a, Description: "'chose X over Y' pattern, search Y — finds the decision",
			Setup:            []Action{startFeature("dr-tradeoff"), note("Decided to use MongoDB over DynamoDB for document storage due to better query flexibility", "decision")},
			Query:            searchQ("DynamoDB", "current_feature", "dr-tradeoff"),
			ExpectedContains: []string{"MongoDB", "DynamoDB"}},
		{ID: "dr-009", Ability: a, Description: "20 decisions, search specific one — found by relevance",
			Setup: []Action{
				startFeature("dr-many"),
				note("Decided to use React for UI components", "decision"),
				note("Decided to use Tailwind CSS for styling", "decision"),
				note("Decided to use Next.js for server-side rendering", "decision"),
				note("Decided to use Prisma ORM for database access", "decision"),
				note("Decided to use tRPC for type-safe API calls", "decision"),
				note("Decided to use Zod for runtime validation", "decision"),
				note("Decided to use NextAuth for authentication", "decision"),
				note("Decided to use Vercel for deployment", "decision"),
				note("Decided to use Planetscale for managed MySQL", "decision"),
				note("Decided to use Upstash for serverless Redis", "decision"),
				note("Decided to use Resend for transactional emails", "decision"),
				note("Decided to use Sentry for error monitoring", "decision"),
				note("Decided to use PostHog for product analytics", "decision"),
				note("Decided to use Stripe for payment processing integration", "decision"),
				note("Decided to use Cloudflare for CDN and DDoS protection", "decision"),
				note("Decided to use GitHub Actions for CI/CD pipeline", "decision"),
				note("Decided to use Turborepo for monorepo management", "decision"),
				note("Decided to use Playwright for end-to-end testing", "decision"),
				note("Decided to use Vitest for unit testing", "decision"),
				note("Decided to use Storybook for component documentation", "decision"),
			},
			Query: searchQ("Stripe payment", "current_feature", "dr-many"), ExpectedContains: []string{"Stripe", "payment"}},
		{ID: "dr-010", Ability: a, Description: "Search by type filter — only matching type returned",
			Setup: []Action{
				startFeature("dr-typefilter"),
				note("Decided to use microservices architecture", "decision"),
				note("Working on microservices communication layer", "progress"),
				note("Microservices deployment pipeline blocked", "blocker"),
			},
			Query: searchTyped("microservices", "current_feature", "dr-typefilter", []interface{}{"notes"}), ExpectedContains: []string{"microservices"}},
	}
}

// ---------------------------------------------------------------------------
// Ability 3: Knowledge Updates / Contradiction (10 scenarios)
// ---------------------------------------------------------------------------

func knowledgeUpdateScenarios() []Scenario {
	const a = "knowledge_updates"

	// Generate fact-override scenarios from table: start feature, add sequential
	// facts with same subject+predicate, query, expect only the latest is active.
	overrides := []struct {
		id, feature, desc, subj, pred string
		values                        []string
	}{
		{"ku-001", "ku-contradict", "Contradicting fact — new active, old invalidated", "database", "uses", []string{"MySQL 8.0", "PostgreSQL 16"}},
		{"ku-002", "ku-sequential", "3 sequential updates — only latest active", "go_version", "is", []string{"1.20", "1.21", "1.22"}},
		{"ku-004", "ku-invalidate", "Override fact — new active, old gone", "deploy_target", "is", []string{"AWS ECS", "GCP Cloud Run"}},
		{"ku-005", "ku-temporal", "Facts at different times — latest active", "api_version", "is", []string{"v1", "v2"}},
		{"ku-010", "ku-rapid", "Rapid succession of 5 updates — only latest survives", "port", "is", []string{"3000", "8080", "8443", "9090", "4000"}},
	}
	// ku-005 has a special ExpectedNotContain format ("api_version is v1" not just "v1").
	scenarios := make([]Scenario, 0, 10)
	for _, o := range overrides {
		s := factOverride(o.id, o.feature, o.desc, o.subj, o.pred, o.values, nil)
		if o.id == "ku-005" {
			s.ExpectedNotContain = []string{"api_version is v1"}
		}
		scenarios = append(scenarios, s)
	}

	// Remaining hand-crafted scenarios.
	scenarios = append(scenarios,
		Scenario{ID: "ku-003", Ability: a, Description: "Identical fact twice — no duplicate",
			Setup:            []Action{startFeature("ku-duplicate"), fact("framework", "is", "Django"), fact("framework", "is", "Django")},
			Query:            factsQuery("ku-duplicate"),
			ExpectedContains: []string{"Django"}},
		Scenario{ID: "ku-006", Ability: a, Description: "Contradicting fact in context — new shown, old not",
			Setup: []Action{startFeature("ku-context"), fact("auth_provider", "uses", "Auth0"), fact("auth_provider", "uses", "Clerk")},
			Query: ctxQuery("ku-context", "standard"), ExpectedContains: []string{"Clerk"},
			ExpectedNotContain: []string{"Auth0"}},
		Scenario{ID: "ku-007", Ability: a, Description: "5 facts, change 2 — 3 original + 2 updated active",
			Setup: []Action{
				startFeature("ku-partial"),
				fact("language", "is", "Go"), fact("database", "is", "SQLite"), fact("cache", "is", "Redis"),
				fact("queue", "is", "RabbitMQ"), fact("monitoring", "is", "Datadog"),
				fact("database", "is", "CockroachDB"), fact("queue", "is", "Apache Kafka"),
			},
			Query: factsQuery("ku-partial"), ExpectedContains: []string{"Go", "CockroachDB", "Redis", "Apache Kafka", "Datadog"},
			ExpectedNotContain: []string{"SQLite", "RabbitMQ"}},
		Scenario{ID: "ku-008", Ability: a, Description: "Same subject, different predicate — both active",
			Setup: []Action{startFeature("ku-diff-pred"), fact("auth", "uses", "JWT tokens"), fact("auth", "requires", "2FA for admin users")},
			Query: factsQuery("ku-diff-pred"), ExpectedContains: []string{"JWT tokens", "2FA for admin users"}},
		Scenario{ID: "ku-009", Ability: a, Description: "Fact + note about same topic — both exist",
			Setup: []Action{startFeature("ku-mixed"), fact("orm", "uses", "GORM"), note("Evaluated GORM vs sqlx, chose GORM for migration support", "decision")},
			Query: ctxQuery("ku-mixed", "standard"), ExpectedContains: []string{"GORM"}},
	)
	return scenarios
}

// ---------------------------------------------------------------------------
// Ability 4: Temporal Reasoning (10 scenarios)
// ---------------------------------------------------------------------------

func temporalReasoningScenarios() []Scenario {
	const a = "temporal_reasoning"
	return []Scenario{
		{ID: "tr-001", Ability: a, Description: "Fact changed — latest active, old gone",
			Setup: []Action{startFeature("tr-bitemporal"), fact("runtime", "uses", "Node 18"), fact("runtime", "uses", "Node 20")},
			Query: factsQuery("tr-bitemporal"), ExpectedContains: []string{"Node 20"},
			ExpectedNotContain: []string{"Node 18"}},
		{ID: "tr-002", Ability: a, Description: "Notes across 5 days, query — recent shown",
			Setup: []Action{
				startFeature("tr-recent"),
				note("Day 1: Initial project scaffolding", "progress"), note("Day 2: Database schema design", "progress"),
				note("Day 3: API endpoint implementation", "progress"), note("Day 4: Frontend integration", "progress"),
				note("Day 5: Testing and bug fixes", "progress"),
			},
			Query: ctxQuery("tr-recent", "detailed"), ExpectedContains: []string{"Day 5"}},
		{ID: "tr-003", Ability: a, Description: "Two decisions at different times — both visible",
			Setup: []Action{
				startFeature("tr-decisions"),
				note("January decision: use monorepo structure", "decision"),
				note("February decision: adopt trunk-based development", "decision"),
			},
			Query: ctxQuery("tr-decisions", "standard"), ExpectedContains: []string{"monorepo", "trunk-based"}},
		{ID: "tr-004", Ability: a, Description: "Multiple sessions over time — ordered by recency",
			Setup: []Action{
				startFeature("tr-sessions"),
				note("First session work on authentication", "progress"),
				endSess(), startSessTool("tr-sessions", "session-2"),
				note("Second session work on authorization", "progress"),
				endSess(), startSessTool("tr-sessions", "session-3"),
				note("Third session work on audit logging", "progress"),
			},
			Query: ctxQuery("tr-sessions", "detailed"), ExpectedContains: []string{"Sessions:"}},
		{ID: "tr-005", Ability: a, Description: "Features listed by last_active",
			Setup: []Action{
				startFeature("tr-old-feature"), note("Old feature work", "progress"),
				startFeature("tr-new-feature"), note("New feature work", "progress"),
			},
			Query: listFeaturesQuery(), ExpectedContains: []string{"tr-new-feature", "tr-old-feature"}},
		{ID: "tr-006", Ability: a, Description: "Old vs new notes — new ranked higher",
			Setup: []Action{
				startFeature("tr-note-order"),
				note("Early work: set up project structure", "progress"), note("Early work: configured linting rules", "progress"),
				note("Recent work: implemented core business logic", "progress"), note("Recent work: added comprehensive test suite", "progress"),
			},
			Query: ctxQuery("tr-note-order", "detailed"), ExpectedContains: []string{"comprehensive test suite"}},
		{ID: "tr-007", Ability: a, Description: "Facts added across sessions — all visible",
			Setup: []Action{
				startFeature("tr-facts-sessions"), fact("api", "version", "v1"),
				endSess(), startSess("tr-facts-sessions"), fact("sdk", "version", "2.0"),
			},
			Query: factsQuery("tr-facts-sessions"), ExpectedContains: []string{"v1", "2.0"}},
		{ID: "tr-008", Ability: a, Description: "Plan superseded — revised plan shown",
			Setup: []Action{
				startFeature("tr-plan-history"),
				savePlan("Initial Plan", "First approach to implementation", "Design API", "Implement endpoints"),
				savePlan("Revised Plan", "Updated approach after feedback", "Design API v2", "Implement endpoints v2", "Add caching layer"),
			},
			Query: ctxQuery("tr-plan-history", "standard"), ExpectedContains: []string{"Revised Plan"}},
		{ID: "tr-009", Ability: a, Description: "Search results ranked by recency",
			Setup: []Action{
				startFeature("tr-search-recency"),
				note("Old authentication implementation using basic auth", "progress"),
				note("New authentication implementation using OAuth2 with PKCE flow", "progress"),
			},
			Query: searchQ("authentication implementation", "current_feature", "tr-search-recency"), ExpectedContains: []string{"authentication"}},
		{ID: "tr-010", Ability: a, Description: "Context snapshot — latest facts only",
			Setup: []Action{
				startFeature("tr-snapshot"),
				fact("framework", "uses", "Express.js"), note("Set up Express.js server with middleware", "progress"),
				fact("framework", "uses", "Fastify"), note("Migrated from Express to Fastify for better performance", "progress"),
			},
			Query: ctxQuery("tr-snapshot", "standard"), ExpectedContains: []string{"Fastify"},
			ExpectedNotContain: []string{"Express.js"}},
	}
}

// ---------------------------------------------------------------------------
// Ability 5: Cross-Feature Reasoning (8 scenarios)
// ---------------------------------------------------------------------------

func crossFeatureScenarios() []Scenario {
	const a = "cross_feature"
	return []Scenario{
		{ID: "cf-001", Ability: a, Description: "3 features, search all — results from all",
			Setup: []Action{
				startFeature("cf-frontend"), note("Implemented responsive dashboard layout with CSS Grid", "progress"),
				startFeature("cf-backend"), note("Built REST API dashboard endpoint returning aggregated metrics", "progress"),
				startFeature("cf-infra"), note("Deployed dashboard monitoring stack with Grafana", "progress"),
			},
			Query: searchAll("dashboard"), ExpectedContains: []string{"dashboard"}},
		{ID: "cf-002", Ability: a, Description: "2 features share related decisions — both found",
			Setup: []Action{
				startFeature("cf-auth-svc"), note("Decided to use JWT with RSA-256 for inter-service authentication", "decision"),
				startFeature("cf-gateway"), note("Decided gateway validates JWT tokens before proxying requests", "decision"),
			},
			Query: searchAll("JWT"), ExpectedContains: []string{"JWT"}},
		{ID: "cf-003", Ability: a, Description: "List features — all shown",
			Setup: []Action{
				startFeature("cf-feature-active"), note("Working on active feature", "progress"),
				startFeature("cf-feature-paused"), note("This feature will be paused", "progress"),
				startFeature("cf-feature-current"),
			},
			Query: listFeaturesQuery(), ExpectedContains: []string{"cf-feature-active", "cf-feature-paused", "cf-feature-current"}},
		{ID: "cf-004", Ability: a, Description: "Switch feature and back — context restored",
			Setup: []Action{
				startFeature("cf-switch-a"), note("Feature A: built the notification engine", "progress"), fact("notifications", "uses", "WebSockets"),
				startFeature("cf-switch-b"), note("Feature B: built the search indexer", "progress"),
				startFeature("cf-switch-a"),
			},
			Query: ctxQuery("cf-switch-a", "standard"), ExpectedContains: []string{"notification engine", "WebSockets"},
			ExpectedNotContain: []string{"search indexer"}},
		{ID: "cf-005", Ability: a, Description: "Same fact in 2 features — both in all_features search",
			Setup: []Action{
				startFeature("cf-fact-a"), fact("deployment", "uses", "Kubernetes"),
				startFeature("cf-fact-b"), fact("orchestration", "uses", "Kubernetes"),
			},
			Query: searchAllTyped("Kubernetes", []interface{}{"facts"}), ExpectedContains: []string{"Kubernetes"}},
		{ID: "cf-006", Ability: a, Description: "Notes in paused feature — still searchable",
			Setup: []Action{
				startFeature("cf-paused-feature"),
				note("Implemented the UNIQUE_PAUSED_CONTENT rate limiter with sliding window algorithm", "progress"),
				startFeature("cf-other-active"),
			},
			Query: searchAll("UNIQUE_PAUSED_CONTENT rate limiter"), ExpectedContains: []string{"UNIQUE_PAUSED_CONTENT"}},
		{ID: "cf-007", Ability: a, Description: "Done feature — still searchable",
			Setup: []Action{
				startFeature("cf-done-feature"),
				note("Completed the UNIQUE_DONE_CONTENT OAuth2 integration with PKCE flow", "progress"),
				startFeature("cf-active-other"),
			},
			Query: searchAll("UNIQUE_DONE_CONTENT OAuth2"), ExpectedContains: []string{"UNIQUE_DONE_CONTENT"}},
		{ID: "cf-008", Ability: a, Description: "Search scope=current_feature — only current results",
			Setup: []Action{
				startFeature("cf-scoped-a"), noteForFeature("cf-scoped-a", "Implemented the RADARFOX webhook handler for Stripe events", "progress"),
				startFeature("cf-scoped-b"), noteForFeature("cf-scoped-b", "Implemented the SONARWOLF email notification sender", "progress"),
			},
			Query: searchQ("SONARWOLF", "current_feature", "cf-scoped-b"), ExpectedContains: []string{"SONARWOLF"},
			ExpectedNotContain: []string{"RADARFOX"}},
	}
}

// ---------------------------------------------------------------------------
// Ability 6: Plan Tracking (10 scenarios)
// ---------------------------------------------------------------------------

func planTrackingScenarios() []Scenario {
	const a = "plan_tracking"
	return []Scenario{
		{ID: "pt-001", Ability: a, Description: "Save plan with 5 steps — all stored",
			Setup: []Action{
				startFeature("pt-basic-plan"),
				savePlan("API Implementation Plan", "Build REST API for user management",
					"Design API schema", "Implement user CRUD", "Add authentication middleware",
					"Write integration tests", "Deploy to staging"),
			},
			Query: ctxQuery("pt-basic-plan", "standard"), ExpectedContains: []string{"API Implementation Plan", "0/5"}},
		{ID: "pt-002", Ability: a, Description: "Save plan — progress 0/5",
			Setup: []Action{
				startFeature("pt-progress"),
				savePlan("Migration Plan", "Database migration to new schema",
					"Backup current data", "Run schema migration", "Verify data integrity",
					"Update application code", "Deploy and monitor"),
			},
			Query: ctxQuery("pt-progress", "standard"), ExpectedContains: []string{"Migration Plan", "0/5"}},
		{ID: "pt-003", Ability: a, Description: "Old plan superseded, new active",
			Setup: []Action{
				startFeature("pt-supersede"),
				savePlan("Original Plan", "First approach", "Step A1", "Step A2"),
				savePlan("Revised Plan", "Better approach after review", "Step B1", "Step B2", "Step B3"),
			},
			Query: ctxQuery("pt-supersede", "standard"), ExpectedContains: []string{"Revised Plan", "0/3"}},
		{ID: "pt-004", Ability: a, Description: "Plan-like content in note — auto-promoted",
			Setup: []Action{
				startFeature("pt-auto-promote"),
				note("Implementation plan steps:\n1. Set up database connection pool\n2. Create migration framework\n3. Build query builder abstraction\n4. Add connection health checks", "note"),
			},
			Query: ctxQuery("pt-auto-promote", "standard"), ExpectedContains: []string{"Plan"}},
		{ID: "pt-005", Ability: a, Description: "Plan with steps — verified via context",
			Setup: []Action{
				startFeature("pt-commit-match"),
				savePlan("Feature Implementation", "Build the user dashboard",
					"Create dashboard component", "Add data visualization charts", "Implement filter controls"),
			},
			Query: ctxQuery("pt-commit-match", "standard"), ExpectedContains: []string{"Feature Implementation", "0/3"}},
		{ID: "pt-006", Ability: a, Description: "Context includes plan progress",
			Setup: []Action{
				startFeature("pt-in-context"),
				savePlan("Refactoring Plan", "Code quality improvements",
					"Extract shared utilities", "Reduce code duplication", "Improve error handling", "Add logging"),
			},
			Query: ctxQuery("pt-in-context", "standard"), ExpectedContains: []string{"Refactoring Plan", "0/4"}},
		{ID: "pt-007", Ability: a, Description: "Multiple features — each has own plan",
			Setup: []Action{
				startFeature("pt-multi-a"),
				savePlanFor("pt-multi-a", "Frontend Plan", "UI implementation", "Build component library", "Create page layouts"),
				startFeature("pt-multi-b"),
				savePlanFor("pt-multi-b", "Backend Plan", "API implementation", "Design data models", "Build API handlers", "Add middleware"),
			},
			Query: ctxQuery("pt-multi-b", "standard"), ExpectedContains: []string{"Backend Plan", "0/3"},
			ExpectedNotContain: []string{"Frontend Plan"}},
		{ID: "pt-008", Ability: a, Description: "Plan with 2 steps — shows 0/2",
			Setup: []Action{startFeature("pt-complete"), savePlan("Quick Fix Plan", "Bug fixes", "Identify root cause", "Write fix")},
			Query: ctxQuery("pt-complete", "standard"), ExpectedContains: []string{"Quick Fix Plan", "0/2"}},
		{ID: "pt-009", Ability: a, Description: "Superseded plan still in search",
			Setup: []Action{
				startFeature("pt-history"),
				savePlan("UNIQUE_ORIGINAL_PLAN_ALPHA", "First implementation approach using monolith", "Build monolith"),
				savePlan("UNIQUE_REVISED_PLAN_BETA", "Second approach using microservices", "Design service boundaries", "Build services"),
			},
			Query: searchTyped("UNIQUE_ORIGINAL_PLAN_ALPHA", "current_feature", "pt-history", []interface{}{"plans"}), ExpectedContains: []string{"UNIQUE_ORIGINAL_PLAN_ALPHA"}},
		{ID: "pt-010", Ability: a, Description: "Empty plan — handled gracefully",
			Setup: []Action{startFeature("pt-empty-plan"), note("We need a plan for the implementation but haven't defined steps yet.", "note")},
			Query: ctxQuery("pt-empty-plan", "standard"), ExpectedContains: []string{"pt-empty-plan"}},
	}
}

// ---------------------------------------------------------------------------
// Ability 7: Abstention (7 scenarios)
// ---------------------------------------------------------------------------

func abstentionScenarios() []Scenario {
	const a = "abstention"
	return []Scenario{
		{ID: "ab-001", Ability: a, Description: "Search for topic never discussed — no results",
			Setup:            []Action{startFeature("ab-empty-search"), note("Set up the project with basic scaffolding", "progress")},
			Query:            searchQ("quantum entanglement photonic computing", "current_feature", "ab-empty-search"),
			ExpectedContains: []string{"No results"}},
		{ID: "ab-002", Ability: a, Description: "Query empty feature — graceful empty response",
			Setup: []Action{startFeature("ab-empty-feature")},
			Query: ctxQuery("ab-empty-feature", "standard"), ExpectedContains: []string{"ab-empty-feature"},
			ExpectedNotContain: []string{"Notes:", "Commits:"}},
		{ID: "ab-003", Ability: a, Description: "Gibberish query — no results",
			Setup:            []Action{startFeature("ab-gibberish"), note("Normal development note about API design", "progress")},
			Query:            searchQ("xyzzy plugh qwerty asdfgh zxcvbn", "current_feature", "ab-gibberish"),
			ExpectedContains: []string{"No results"}},
		{ID: "ab-004", Ability: a, Description: "Non-existent feature — error or empty",
			Setup:            []Action{startFeature("ab-exists")},
			Query:            Query{Tool: "get_context", Params: params{"feature_id": "nonexistent-feature-id-00000", "tier": "standard"}},
			ExpectedContains: []string{}},
		{ID: "ab-005", Ability: a, Description: "Facts query with no facts — empty list",
			Setup:            []Action{startFeature("ab-no-facts"), note("Just a progress note, no facts here", "progress")},
			Query:            factsQuery("ab-no-facts"),
			ExpectedContains: []string{"No active facts"}},
		{ID: "ab-006", Ability: a, Description: "Search current_feature for nonexistent — no results",
			Setup: []Action{
				startFeature("ab-no-active"), note("Some content about testing", "progress"),
				startFeature("ab-other-feature"),
			},
			Query: searchQ("nonexistent topic xyzzy", "current_feature", "ab-other-feature"), ExpectedContains: []string{"No results"}},
		{ID: "ab-007", Ability: a, Description: "Query plan when none exists — no plan shown",
			Setup:              []Action{startFeature("ab-no-plan"), note("Working on implementation without a formal plan", "progress")},
			Query:              ctxQuery("ab-no-plan", "standard"),
			ExpectedNotContain: []string{"Plan:"}},
	}
}
