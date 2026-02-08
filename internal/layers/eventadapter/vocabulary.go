package eventadapter

// Vocabulary holds the controlled vocabulary for event normalization
type Vocabulary struct {
	Status  map[string]string // keyword -> normalized term
	Surface map[string]string // keyword -> normalized term
	Flow    map[string]string // keyword -> normalized term
	Noise   map[string]bool   // words to ignore
}

// NewVocabulary creates a vocabulary with default mappings
func NewVocabulary() *Vocabulary {
	return &Vocabulary{
		Status: map[string]string{
			// Success states
			"success":   "success",
			"succeeded": "success",
			"ok":        "success",
			"complete":  "success",
			"completed": "success",
			"done":      "success",

			// Failure states
			"failed":  "failed",
			"failure": "failed",
			"error":   "error",
			"err":     "error",

			// Pending states
			"pending":    "pending",
			"waiting":    "pending",
			"processing": "pending",
			"loading":    "pending",

			// Interaction states
			"click":   "click",
			"clicked": "click",
			"tap":     "click",
			"tapped":  "click",
			"press":   "click",
			"pressed": "click",

			// View states
			"view":    "view",
			"viewed":  "view",
			"seen":    "view",
			"visible": "view",
			"show":    "view",
			"shown":   "view",
			"display": "view",

			// Dismiss states
			"dismiss":   "dismiss",
			"dismissed": "dismiss",
			"close":     "dismiss",
			"closed":    "dismiss",
			"hide":      "dismiss",
			"hidden":    "dismiss",
			"exit":      "exit",
			"exited":    "exit",
			"abandon":   "exit",
			"abandoned": "exit",

			// Start/End states
			"start":   "start",
			"started": "start",
			"begin":   "start",
			"began":   "start",
			"end":     "end",
			"ended":   "end",
			"finish":  "end",
			"stop":    "end",
			"stopped": "end",
		},

		Surface: map[string]string{
			// Navigation
			"home":       "home",
			"homepage":   "home",
			"index":      "home",
			"landing":    "home",
			"nav":        "navigation",
			"navbar":     "navigation",
			"navigation": "navigation",
			"menu":       "navigation",
			"sidebar":    "sidebar",
			"header":     "header",
			"footer":     "footer",

			// Commerce
			"checkout":  "checkout",
			"cart":      "cart",
			"basket":    "cart",
			"product":   "product",
			"item":      "product",
			"catalog":   "catalog",
			"shop":      "shop",
			"store":     "shop",

			// UI Components
			"modal":    "modal",
			"dialog":   "modal",
			"popup":    "modal",
			"popover":  "modal",
			"tooltip":  "tooltip",
			"drawer":   "drawer",
			"panel":    "panel",
			"card":     "card",
			"tile":     "card",
			"list":     "list",
			"table":    "table",
			"grid":     "grid",
			"form":     "form",
			"input":    "input",
			"field":    "input",
			"button":   "button",
			"btn":      "button",
			"cta":      "button",
			"link":     "link",
			"tab":      "tab",
			"tabs":     "tab",
			"dropdown": "dropdown",
			"select":   "dropdown",
			"banner":   "banner",
			"alert":    "alert",
			"toast":    "toast",
			"snackbar": "toast",

			// Screens/Pages
			"page":      "page",
			"screen":    "screen",
			"view":      "view",
			"dashboard": "dashboard",
			"settings":  "settings",
			"profile":   "profile",
			"account":   "account",
			"search":    "search",
			"results":   "results",
			"detail":    "detail",
			"details":   "detail",
		},

		Flow: map[string]string{
			// Authentication
			"login":        "authentication",
			"signin":       "authentication",
			"sign":         "authentication",
			"logout":       "authentication",
			"signout":      "authentication",
			"auth":         "authentication",
			"authenticate": "authentication",

			// Registration
			"register":     "registration",
			"registration": "registration",
			"signup":       "registration",
			"onboard":      "onboarding",
			"onboarding":   "onboarding",

			// CRUD - Creation
			"create":   "creation",
			"created":  "creation",
			"add":      "creation",
			"added":    "creation",
			"new":      "creation",
			"insert":   "creation",
			"inserted": "creation",
			"post":     "creation",
			"submit":   "creation",
			"submitted": "creation",

			// CRUD - Read
			"read":      "retrieval",
			"get":       "retrieval",
			"fetch":     "retrieval",
			"load":      "retrieval",
			"loaded":    "retrieval",
			"retrieve":  "retrieval",
			"retrieved": "retrieval",

			// CRUD - Update
			"update":  "update",
			"updated": "update",
			"edit":    "update",
			"edited":  "update",
			"modify":  "update",
			"change":  "update",
			"changed": "update",
			"save":    "update",
			"saved":   "update",

			// CRUD - Delete
			"delete":  "deletion",
			"deleted": "deletion",
			"remove":  "deletion",
			"removed": "deletion",
			"cancel":  "cancellation",
			"canceled": "cancellation",
			"cancelled": "cancellation",

			// Search
			"search":   "search",
			"searched": "search",
			"find":     "search",
			"filter":   "filter",
			"filtered": "filter",
			"sort":     "sort",
			"sorted":   "sort",

			// Commerce flows
			"checkout": "checkout",
			"payment":  "payment",
			"pay":      "payment",
			"purchase": "purchase",
			"buy":      "purchase",
			"order":    "order",
			"ordered":  "order",
			"refund":   "refund",

			// Sharing
			"share":    "sharing",
			"shared":   "sharing",
			"invite":   "sharing",
			"invited":  "sharing",
			"export":   "export",
			"exported": "export",
			"download": "download",
			"upload":   "upload",
			"uploaded": "upload",

			// Navigation flows
			"navigate":  "navigation",
			"navigated": "navigation",
			"redirect":  "navigation",
			"scroll":    "scroll",
			"scrolled":  "scroll",
			"swipe":     "swipe",
			"swiped":    "swipe",
		},

		Noise: map[string]bool{
			"the":     true,
			"a":       true,
			"an":      true,
			"user":    true,
			"users":   true,
			"id":      true,
			"ids":     true,
			"v1":      true,
			"v2":      true,
			"v3":      true,
			"api":     true,
			"event":   true,
			"events":  true,
			"data":    true,
			"info":    true,
			"test":    true,
			"debug":   true,
			"log":     true,
			"track":   true,
			"tracked": true,
			"action":  true,
			"type":    true,
			"name":    true,
			"value":   true,
			"is":      true,
			"on":      true,
			"in":      true,
			"to":      true,
			"for":     true,
			"with":    true,
			"by":      true,
			"of":      true,
			"at":      true,
		},
	}
}

// AddStatus adds a custom status keyword mapping
func (v *Vocabulary) AddStatus(keyword, normalized string) {
	v.Status[keyword] = normalized
}

// AddSurface adds a custom surface keyword mapping
func (v *Vocabulary) AddSurface(keyword, normalized string) {
	v.Surface[keyword] = normalized
}

// AddFlow adds a custom flow keyword mapping
func (v *Vocabulary) AddFlow(keyword, normalized string) {
	v.Flow[keyword] = normalized
}

// AddNoise adds a word to the noise list
func (v *Vocabulary) AddNoise(word string) {
	v.Noise[word] = true
}
