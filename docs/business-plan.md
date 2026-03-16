# AIRelay — Business Plan

**Date:** 2026-03-16
**Stage:** Pre-build
**Goal:** $10k MRR → financial independence for a solo founder

---

## 1. What We're Building

AIRelay is an AI API gateway that protects developers from surprise bills by enforcing real-time cost budgets across OpenAI, Anthropic, Google, and other providers. One environment variable change integrates it into any existing codebase.

Revenue comes from the hosted cloud product — dashboard, alerts, team features, and SLA. The proxy engine is source-available under BSL 1.1.

---

## 2. Revenue Model

Three tiers, self-serve, no sales calls required.

| Tier | Price | Key Gate |
|---|---|---|
| Free | $0 | 1 project, 7-day history, email alerts |
| Pro | $79/mo | Unlimited projects, 90-day history, webhooks, metadata attribution |
| Team | $199/mo | 5 seats, unlimited history, billing webhooks, 99.9% SLA |
| Enterprise *(future)* | $500-2,000/mo | Custom limits, SSO, dedicated support, custom SLA |

**Natural upgrade triggers:**
- Free → Pro: second project, need last month's data, want webhook alerts
- Pro → Team: teammate needs access, need to pass AI costs to their own customers
- Team → Enterprise: company-wide rollout, compliance requirements, dedicated support

---

## 3. Unit Economics

### Gross Margins

Infrastructure is the only variable cost. The proxy is a stateless Go service — marginal cost per additional customer is near zero.

| MRR | Infra cost | Gross margin |
|---|---|---|
| $1,000 | ~$30/mo | ~97% |
| $5,000 | ~$75/mo | ~98.5% |
| $10,000 | ~$150/mo | ~98.5% |
| $50,000 | ~$500/mo | ~99% |

This is one of the highest-margin business models possible. Every dollar of revenue above the first few hundred is nearly pure profit.

### Customer Acquisition Cost (CAC)

Target: < $50/customer via inbound/content. Most developer tools that succeed do so without paid ads — word of mouth, GitHub, Hacker News, and SEO drive the majority of signups. At zero paid marketing spend, CAC is effectively time.

### Churn

Developer infrastructure tools have lower churn than consumer products because:
- Integrating into production creates switching friction
- Engineers don't regularly audit and cancel developer tools
- Target monthly churn: 3-5%
- Target net revenue retention: > 100% (free → paid → team expansion)

---

## 4. Revenue Projections

### The $10k MRR Math

The simplest path: **~100-130 paying customers.**

| Scenario | Mix | MRR |
|---|---|---|
| All Pro | 127 customers × $79 | $10,033 |
| Realistic mix | 80 Pro + 25 Team | $10,295 |
| With early Enterprise | 60 Pro + 20 Team + 3 Enterprise ($500) | $10,220 |

### Floor Estimates (conservative — slow, steady inbound growth)

Assumes: no viral moment, organic growth only, 2-3% free-to-paid conversion.

| Month | Free users | Paying customers | MRR |
|---|---|---|---|
| 3 | 100 | 2 Pro | $158 |
| 6 | 300 | 6 Pro + 1 Team | $673 |
| 12 | 700 | 16 Pro + 3 Team | $1,861 |
| 18 | 1,200 | 30 Pro + 7 Team | $3,763 |
| 24 | 2,000 | 52 Pro + 14 Team + 2 Enterprise | $7,894 |
| 30 | 3,000 | 78 Pro + 22 Team + 4 Enterprise | $12,540 |

**Floor conclusion:** $10k MRR in approximately 26-30 months on organic growth alone.

### Ceiling Estimates (optimistic — HN front page, strong word of mouth)

Assumes: successful Show HN launch, a few high-profile developer advocates, 5% free-to-paid conversion.

| Month | Free users | Paying customers | MRR |
|---|---|---|---|
| 3 | 500 | 15 Pro + 2 Team | $1,583 |
| 6 | 1,500 | 40 Pro + 8 Team | $4,752 |
| 12 | 4,000 | 100 Pro + 25 Team + 5 Enterprise | $15,375 |
| 18 | 8,000 | 180 Pro + 60 Team + 12 Enterprise | $32,120 |
| 24 | 14,000 | 300 Pro + 120 Team + 25 Enterprise | $61,580 |

**Ceiling conclusion:** $10k MRR achievable within 6-9 months with a strong launch.

### Realistic Target

Somewhere between floor and ceiling. **$10k MRR within 18 months** is an achievable goal that doesn't require going viral — just consistent execution on distribution and a product that works.

---

## 5. Financial Milestones

| MRR | Significance |
|---|---|
| $500 | Covers infrastructure costs — product pays for itself |
| $2,000 | Side income meaningful — supplements other income |
| $5,000 | Part-time equivalent income — reduces external pressure |
| $10,000 | Full financial independence — the primary goal |
| $20,000 | Comfortable. Start thinking about Enterprise tier and first contractor |
| $50,000+ | Small team, accelerated growth |

---

## 6. Marketing Strategy

**No cold outreach. No paid ads at launch. Purely inbound and community.**

### Phase 1 — Pre-launch (during build, weeks 1-8)

**Build in public:**
- Twitter/X: document the build weekly. Share insights about AI costs ("we analyzed 10k LLM requests and here's what we found"). Developers follow builders.
- GitHub: get the repo up early, clean README, star-worthy from day one

**SEO foundation:**
- Write 3-5 long-form articles targeting high-intent developer searches:
  - "how to prevent OpenAI bill shock"
  - "track OpenAI API costs per user"
  - "AI API cost management for SaaS"
  - "OpenAI budget limits how to"
- These take 3-6 months to rank but compound over time

### Phase 2 — Launch (week 8-10)

Priority order:

1. **Show HN** — the single highest-leverage launch channel for developer tools. A good Show HN with a working product can drive thousands of signups in 48 hours. Write the post carefully: lead with the problem, be specific about how it works, include real numbers.

2. **ProductHunt** — secondary to HN for developer tools but still worthwhile. Schedule for a Tuesday-Thursday launch.

3. **Community posts:**
   - r/SideProject, r/webdev, r/MachineLearning
   - Indie Hackers (strong audience for bootstrapped products)
   - Relevant Discord servers (AI builders, LangChain, etc.)

4. **AI newsletters:** reach out to Latent Space, Ben's Bites, The Rundown for a mention. Many cover useful developer tools for free.

### Phase 3 — Growth (month 3+)

**Content marketing:**
- Case studies: "How [company] reduced their OpenAI bill by 40%"
- Integration tutorials: "Use AIRelay with LangChain", "Use AIRelay with the Vercel AI SDK"
- These tutorials live on the AIRelay blog and rank for integration-specific searches

**Integrations as distribution:**
- Submit to LangChain, LlamaIndex, CrewAI integration directories
- Add AIRelay to "awesome-llm-tools" type GitHub lists
- Developer tools that appear in framework docs get organic signups indefinitely

**Referral program (month 6+):**
- Free users get 1 extra month of history per referral that converts to paid
- Simple, not aggressive — just a nudge

**Word of mouth:**
- The best marketing for a developer tool is a product that works and is simple to use. Engineers talk to each other. One happy user at a 20-person startup can bring the whole team.

---

## 7. Competitive Landscape

| Competitor | Strength | Weakness | Our angle |
|---|---|---|---|
| Helicone | YC-backed, good brand | Shifting to evals, getting complex, enterprise-priced | Simpler, cheaper, cost-protection-first |
| Portkey | Feature-rich, routing | Complex setup, enterprise-focused | 5-minute setup, solo-dev friendly |
| LiteLLM | Popular open source | Self-hosted only, requires ops knowledge | Managed service, zero infra to run |
| OpenAI native limits | Built-in, free | Only works for OpenAI, no cross-provider view | Multi-provider, single dashboard |

**Defensibility over time:**
- Usage data moat: aggregate anonymized cost patterns across thousands of customers becomes proprietary insight no competitor can replicate
- Integration lock-in: once the proxy is in production, switching requires an engineering task
- Brand in the cost-protection niche: being known as "the tool that stops surprise AI bills" is a durable position

---

## 8. Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Providers add native budget controls | Medium | Multi-provider value persists; observability/analytics layer differentiates |
| Helicone pivots to compete directly | Medium | Execute faster; brand in cost-protection niche |
| Distribution fails, no signups | Medium | Build in public creates audience before launch; HN is a real channel |
| Proxy downtime damages trust | Low | Fail-open design, multi-region infra, strong SLA messaging |
| Solo founder burnout | Medium | Keep scope tight; ship an MVP, not a perfect product |

---

## 9. 90-Day Plan

**Days 1-30 — Foundation**
- Initialize Go repo, scaffold both services (proxy + management API)
- Auth, projects, API key management
- Core proxy: forward requests, count tokens, log to Postgres
- Basic Redis budget check (no enforcement yet)

**Days 31-60 — Core product**
- Full budget enforcement (hard limit + soft alerts)
- Dashboard v1: cost charts, budget progress, usage breakdown
- Email alerts on threshold breach
- Stripe billing integration

**Days 61-90 — Launch**
- Multi-provider support (OpenAI + Anthropic at minimum)
- Streaming fully tested
- Onboarding flow polished
- Show HN post
- First paying customers

---

## 10. The Independence Math

At $10k MRR with ~99% gross margins:
- Revenue: $10,000/mo
- Infrastructure: ~$150/mo
- Stripe fees (2.9% + $0.30): ~$300/mo
- Net: **~$9,550/mo**

No employees. No office. No investors to answer to. That's the goal.
