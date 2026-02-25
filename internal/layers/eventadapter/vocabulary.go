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
			"open":    "open",
			"opened":  "open",
			"end":     "end",
			"ended":   "end",
			"finish":  "end",
			"stop":    "end",
			"stopped": "end",

			// Resolution states
			"resolve":      "resolve",
			"resolved":     "resolve",
			"verify":       "success",
			"verified":     "success",
			"verification": "verify",
			"unverified":   "failed",

			// Attempt/Initiation states
			"attempt":   "attempt",
			"attempted": "attempt",
			"try":       "attempt",
			"tried":     "attempt",
			"retry":     "attempt",
			"initiate":  "start",
			"initiated": "start",
			"trigger":   "start",
			"triggered": "start",

			// Confirmation/Placement states
			"confirm":   "success",
			"confirmed": "success",
			"place":     "success",
			"placed":    "success",
			"accept":    "success",
			"accepted":  "success",
			"approve":   "success",
			"approved":  "success",

			// Rejection/Denial states
			"reject":   "failed",
			"rejected": "failed",
			"deny":     "failed",
			"denied":   "failed",
			"decline":  "failed",
			"declined": "failed",
			"timeout":  "failed",

			// Cancellation states (status, not flow — "driver_cancelled" means
			// "the driver flow had a cancellation", not "the user navigated to cancellation flow")
			"cancel":    "cancelled",
			"canceled":  "cancelled",
			"cancelled": "cancelled",

			// Assignment/availability states
			"assign":      "assigned",
			"assigned":    "assigned",
			"unassigned":  "unassigned",
			"available":   "available",
			"unavailable": "unavailable",
			"estimated":   "estimate",
			"estimate":    "estimate",

			// Request states
			"request":   "request",
			"requested": "request",

			// Apply states
			"apply":   "apply",
			"applied": "apply",

			// Scroll/interaction states
			"scroll":    "scroll",
			"scrolled":  "scroll",
			"swipe":     "swipe",
			"swiped":    "swipe",
			"select":    "select",
			"selected":  "select",
			"expand":    "expand",
			"expanded":  "expand",
			"collapse":  "collapse",
			"collapsed": "collapse",
			"browse":    "view",
			"browsed":   "view",
			"browsing":  "view",

			// CRUD action states — these are user actions, not flow categories.
			// The flow name comes from the target noun (e.g. "add_to_cart" → cart).
			"add":       "add",
			"added":     "add",
			"create":    "create",
			"created":   "create",
			"insert":    "add",
			"inserted":  "add",
			"remove":    "remove",
			"removed":   "remove",
			"delete":    "remove",
			"deleted":   "remove",
			"update":    "update",
			"updated":   "update",
			"edit":      "update",
			"edited":    "update",
			"modify":    "update",
			"change":    "update",
			"changed":   "update",
			"save":      "save",
			"saved":     "save",
			"submit":    "submit",
			"submitted": "submit",
			"post":      "submit",
			"upload":    "upload",
			"uploaded":  "upload",
			"download":  "download",
			"share":     "share",
			"shared":    "share",
			"invite":    "share",
			"invited":   "share",
			"export":    "export",
			"exported":  "export",
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
			"cart":       "cart",
			"basket":     "cart",
			"product":    "product",
			"item":       "product",
			"catalog":    "catalog",
			"shop":       "shop",
			"store":      "shop",
			"restaurant": "restaurant",
			"delivery":   "delivery",
			"eta":        "delivery",
			"promo":      "promo",
			"coupon":     "promo",
			"discount":   "promo",
			"voucher":    "promo",

			// Ride-hailing / logistics
			"ride":         "ride",
			"rides":        "ride",
			"trip":         "ride",
			"trips":        "ride",
			"booking":      "booking",
			"bookings":     "booking",
			"reservation":  "booking",
			"reservations": "booking",
			"driver":       "driver",
			"drivers":      "driver",
			"rider":        "rider",
			"riders":       "rider",
			"destination":  "destination",
			"pickup":       "pickup",
			"dropoff":      "dropoff",

			// Streaming / Media
			"playback":  "playback",
			"player":    "playback",
			"stream":    "playback",
			"streaming": "playback",
			"video":     "video",
			"audio":     "audio",
			"media":     "media",
			"content":   "content",
			"episode":   "episode",
			"movie":     "movie",
			"watch":     "video",
			"listen":    "audio",
			"queue":     "queue",
			"playlist":  "playlist",
			"channel":   "channel",
			"category":  "category",
			"genre":     "genre",

			// Application lifecycle
			"app":         "app",
			"application": "app",

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
			"banner":   "banner",
			"alert":    "alert",
			"toast":    "toast",
			"snackbar": "toast",

			// Screens/Pages
			"page":      "page",
			"screen":    "screen",
			"dashboard": "dashboard",
			"settings":  "settings",
			"profile":   "profile",
			"account":   "account",
			"results":   "results",
			"detail":    "detail",
			"details":   "detail",

			// Session
			"session":      "session",
			"wizard":       "wizard",
			"step":         "step",
			"notification": "notification",
			"inbox":        "inbox",
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
			"new": "creation",

			// CRUD - Read
			"read":      "retrieval",
			"get":       "retrieval",
			"fetch":     "retrieval",
			"load":      "retrieval",
			"loaded":    "retrieval",
			"retrieve":  "retrieval",
			"retrieved": "retrieval",

			// CRUD - Update

			// CRUD - Delete (cancel moved to Status — see Status section)

			// Search
			"search":   "search",
			"searched": "search",
			"find":     "search",
			"filter":   "filter",
			"filtered": "filter",
			"sort":     "sort",
			"sorted":   "sort",

			// Commerce flows
			"checkout":  "checkout",
			"payment":   "payment",
			"pay":       "payment",
			"purchase":  "purchase",
			"buy":       "purchase",
			"order":     "order",
			"ordered":   "order",
			"refund":    "refund",
			"cart":      "cart",
			"basket":    "cart",
			"wishlist":  "wishlist",
			"favorites": "wishlist",
			"coupon":    "promo",
			"promo":     "promo",
			"voucher":   "promo",

			// Sharing

			// Document flows
			"doc":        "document",
			"document":   "document",
			"documents":  "document",
			"file":       "document",
			"files":      "document",
			"comment":    "commenting",
			"commenting": "commenting",
			"annotate":   "commenting",
			"annotation": "commenting",

			// Navigation flows
			"navigate":  "navigation",
			"navigated": "navigation",
			"redirect":  "navigation",

			// Subscription / Billing
			"subscribe":    "subscription",
			"subscription": "subscription",
			"unsubscribe":  "subscription",
			"renew":        "subscription",
			"renewal":      "subscription",
			"billing":      "billing",
			"invoice":      "billing",
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
			"no":      true,
			"not":     true,
			"max":     true,
			"wall":    true,
			"total":   true,
			"count":   true,
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
