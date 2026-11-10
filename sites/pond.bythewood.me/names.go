package main

import (
	"database/sql"
	"math/rand/v2"
	"strings"
	"time"
)

// Nobody types a frog's name. They roll names off these lists until one fits,
// so a name is always one somebody here would be happy to see on a share card.
var namePrefixes = []string{
	"OLD", "LITTLE", "BIG", "SIR", "LADY", "CAPTAIN", "LORD", "MISS", "MISTER",
	"DOCTOR", "AUNT", "UNCLE", "YOUNG", "TINY", "GRAND", "SAINT", "PROFESSOR",
	"BARON", "DAME", "COUNT", "SQUIRE", "MAJOR", "WEE", "LUCKY",
}

var nameBases = []string{
	// The names a seed can give, which have to stay on the list.
	"PIP", "BRINDLE", "MOSS", "DOT", "MARSH", "TULLY", "BEAN", "SEDGE", "WREN",
	"PUDDLE", "FENN", "CLOVER", "RUSH", "NEWT", "BRAMBLE", "OTIS",
	// The pond and the bank.
	"REED", "LILY", "ALDER", "WILLOW", "HAZEL", "ROWAN", "BIRCH", "ASPEN",
	"FERN", "BRACKEN", "HEATHER", "THISTLE", "TANSY", "SORREL", "YARROW",
	"MALLOW", "BRYONY", "CRESS", "BASIL", "SAGE", "THYME", "PARSLEY", "MINT",
	"JUNIPER", "LAUREL", "IVY", "POPPY", "DAISY", "VIOLET", "PRIMROSE",
	"BLUEBELL", "BUTTERCUP", "COWSLIP", "FOXGLOVE", "LUPIN", "TULIP", "ACORN",
	"CONKER", "HOLLY", "BERRY", "SLOE", "DAMSON", "QUINCE", "MEDLAR", "PEBBLE",
	"COBBLE", "SHINGLE", "GRAVEL", "SILT", "LOAM", "CLAY", "FLINT", "AGATE",
	"JASPER", "OPAL", "AMBER", "GARNET", "BERYL", "ONYX", "COPPER", "TIN",
	"BRASS", "RUSTY", "SOOTY", "MISTY", "FOGGY", "DEWY", "DRIZZLE", "STORMY",
	"THUNDER", "BREEZE", "GUST", "RIPPLE", "SPLASH", "SPLOSH", "PLOP", "PLINK",
	"DRIP", "DROPLET", "BUBBLE", "FROTH", "EDDY", "BROOK", "BECK", "BURN",
	"RILL", "TARN", "MERE", "FORD", "WEIR", "SLUICE", "BOGLE", "FEN", "BOG",
	"MIRE", "WADE", "PADDLE", "DABBLE", "DIPPER", "KINGFISHER", "HERON",
	"SNIPE", "TEAL", "WIGEON", "COOT", "GREBE", "PLOVER", "LAPWING", "CURLEW",
	"DUNLIN", "SWIFT", "MARTIN", "ROBIN", "FINCH", "LINNET", "SISKIN",
	"WAGTAIL", "STARLING", "MAGPIE", "JAY", "ROOK", "OWL", "MOTH", "BEETLE",
	"CRICKET", "MAYFLY", "GNAT", "MIDGE", "SKIPPER", "DRAGONFLY", "DAMSEL",
	"STRIDER", "WATERBOATMAN", "SNAIL", "WHELK", "LIMPET", "WINKLE",
	"MINNOW", "STICKLEBACK", "LOACH", "GUDGEON", "ROACH", "CHUB", "DACE",
	"PERCH", "TENCH", "PIKE", "EEL", "OTTER", "VOLE", "SHREW", "MOLE",
	"BADGER", "STOAT", "HEDGEHOG", "TOAD", "TADPOLE", "SPAWN", "HOPPER",
	"LEAPER", "BOUNDER", "SKIP", "SCAMPER", "TUMBLE", "WOBBLE", "WIGGLE",
	"JIGGLE", "FIDGET", "RUMBLE", "MUMBLE", "BUMBLE", "GRUMBLE", "CROAK",
	"CROAKER", "RIBBIT", "GULP", "BURP", "HICCUP", "SNOOZE", "DOZY", "SLEEPY",
	"SUNNY", "SPECKLE", "FRECKLE", "SPOT", "PATCH", "STRIPE", "SMUDGE",
	"DOODLE", "SCRIBBLE", "NOODLE", "DUMPLING", "MUFFIN", "CRUMPET", "SCONE",
	"BISCUIT", "PUDDING", "TOFFEE", "FUDGE", "TREACLE", "HONEY", "MAPLE",
	"NUTMEG", "GINGER", "PEPPER", "PICKLE", "GHERKIN", "OLIVE", "PEA",
	"SPROUT", "TURNIP", "PARSNIP", "RADISH", "CABBAGE", "KALE", "LEEK",
	"CHIVE", "SHALLOT", "MARROW", "GOURD", "PUMPKIN", "SQUASH", "PEANUT",
	"WALNUT", "CASHEW", "ALMOND", "PISTACHIO", "MANGO", "KIWI", "LIME",
	"LEMON", "PLUM", "FIG", "DATE", "CHERRY", "MELON", "WAFFLE", "PANCAKE",
	"BAGEL", "PRETZEL", "TRUFFLE", "MOCHI", "SPRINKLE", "JELLYBEAN", "GUMDROP",
	"BUTTON", "THIMBLE", "BOBBIN", "SPOOL", "TASSEL", "MITTEN", "SOCK",
	"SLIPPER", "WELLY", "BRIDGET", "BARNABY", "BARTHOLOMEW", "CORNELIUS",
	"ERASMUS", "FERDINAND", "GULLIVER", "HORACE", "IGNATIUS", "LEOPOLD",
	"MONTY", "NIGEL", "OSWALD", "PERCIVAL", "QUENTIN", "RUPERT",
	"SEBASTIAN", "THEODORE", "ULYSSES", "WALDO", "WILBUR", "WINSTON", "ZEBEDEE",
	"AGATHA", "BEATRIX", "CLEMENTINE", "DOROTHEA", "EDNA", "FLORENCE",
	"GERTRUDE", "HENRIETTA", "IMOGEN", "JOSEPHINE", "MATILDA", "NELL",
	"PRUDENCE", "ROSALIND", "TABITHA", "URSULA", "WINIFRED",
	"MABEL", "MAUDE", "ELSIE", "HATTIE", "MOLLY", "POLLY", "TILLY", "MILLIE",
	"ALFIE", "ARCHIE", "BERTIE", "CHARLIE", "FREDDIE", "HUGO", "IGGY",
	"JOJO", "KIPPER", "LOUIE", "MILO", "OLLIE", "PADDY", "REGGIE", "SONNY",
	"TEDDY", "WALLY", "ZIGGY", "BOO", "BOJANGLES", "BINGO", "BONGO", "BOING",
	"ZIPPY", "ZOOM", "COMET", "ROCKET", "NOVA", "COSMO", "ORBIT", "PIXEL",
	"GIZMO", "WIDGET", "SPROCKET", "GADGET", "TOGGLE", "BLIP", "BLOOP",
	"DOOZY", "GOOBER", "GUMBO", "JAMBALAYA", "TUMBLEWEED", "SNICKERDOODLE",
}

const nameRolls = 6

// namePool has every name a frog can be given, so a pick is checked against it.
var namePool = func() map[string]bool {
	m := map[string]bool{}
	for _, b := range nameBases {
		m[b] = true
		for _, p := range namePrefixes {
			m[p+" "+b] = true
		}
	}
	return m
}()

// rollName is a base, and a prefix about one time in three.
func rollName() string {
	b := nameBases[rand.IntN(len(nameBases))]
	if rand.IntN(3) == 0 {
		return namePrefixes[rand.IntN(len(namePrefixes))] + " " + b
	}
	return b
}

func rollNames() []string {
	out := make([]string, 0, nameRolls)
	seen := map[string]bool{}
	for len(out) < nameRolls {
		if n := rollName(); !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

func validName(n string) bool { return namePool[n] }

// rename gives one of the pond's frogs a name off the list.
func (s *store) rename(id, frogID int64, name string) (*view, error) {
	name = strings.ToUpper(strings.Join(strings.Fields(name), " "))
	if !validName(name) {
		return nil, errBadName
	}
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		res, err := tx.Exec(`UPDATE frogs SET name = ? WHERE pond_id = ? AND id = ?`, name, p.ID, frogID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errNotHeld
		}
		if frogID == p.FrogID {
			p.FrogName = name
		}
		return nil
	})
}
