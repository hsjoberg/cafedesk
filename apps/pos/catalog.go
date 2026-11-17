package main

import "cafedesk/pkg/money"

// Category is a group of items, shown as a tab.
type Category struct {
	ID       string
	Name     string
	NameAR   string
	Color    string // the signage band, as a hex color
	Position int
	Kitchen  bool // its items print on the kitchen ticket unless an item says otherwise
}

// Item is something on the menu.
type Item struct {
	ID         string
	CategoryID string
	Name       string
	NameAR     string // Arabic name, shown under the English one
	Price      money.Fils
	Position   int
	Kitchen    bool     // printed on the kitchen ticket
	Groups     []string // modifier groups, in order
}

// ModifierGroup is a choice made for an item, as its sugar level.
type ModifierGroup struct {
	ID        string
	Name      string
	NameAR    string
	Multiple  bool // several modifiers may be chosen, as extras; else exactly one
	Position  int
	Modifiers []Modifier
}

// Modifier is one choice of a group.
type Modifier struct {
	ID       string
	GroupID  string
	Name     string
	NameAR   string
	Delta    money.Fils // added to the item's price
	Default  bool
	Position int
}

// Catalog is the menu the terminal sells from. It lives in the local
// database; from Phase 2 the server sends it down.
type Catalog struct {
	Categories []Category
	Items      []Item
	Groups     map[string]ModifierGroup
}

// ItemsIn returns the items of a category, in menu order.
func (c *Catalog) ItemsIn(categoryID string) []Item {
	var out []Item
	for _, it := range c.Items {
		if it.CategoryID == categoryID {
			out = append(out, it)
		}
	}
	return out
}

// Category returns a category by ID.
func (c *Catalog) Category(id string) (Category, bool) {
	for _, cat := range c.Categories {
		if cat.ID == id {
			return cat, true
		}
	}
	return Category{}, false
}

// GroupsOf returns an item's modifier groups, in order.
func (c *Catalog) GroupsOf(it Item) []ModifierGroup {
	var out []ModifierGroup
	for _, id := range it.Groups {
		if g, ok := c.Groups[id]; ok {
			out = append(out, g)
		}
	}
	return out
}

// DefaultMods returns the modifiers an item gets when it is tapped.
func (c *Catalog) DefaultMods(it Item) []LineMod {
	var out []LineMod
	for _, g := range c.GroupsOf(it) {
		for _, m := range g.Modifiers {
			if m.Default {
				out = append(out, lineMod(g, m))
			}
		}
	}
	return out
}

func lineMod(g ModifierGroup, m Modifier) LineMod {
	return LineMod{GroupID: g.ID, GroupName: g.Name, ID: m.ID, Name: m.Name, NameAR: m.NameAR, Delta: m.Delta,
		Default: m.Default}
}

// demoCatalog is the placeholder menu a new terminal starts with, so the
// screens show real-sized content. Prices and Arabic names are examples to
// be corrected; from Phase 2 the admin replaces it.
func demoCatalog() *Catalog {
	c := &Catalog{
		Categories: []Category{
			{ID: "tea", Name: "Tea", NameAR: "شاي", Color: "#5F8A3A"},
			{ID: "meals", Name: "Meals", NameAR: "وجبات", Color: "#2F6690", Kitchen: true},
			{ID: "snacks", Name: "Snacks", NameAR: "وجبات خفيفة", Color: "#7B4B94", Kitchen: true},
			{ID: "drinks", Name: "Drinks", NameAR: "مشروبات", Color: "#1E8A83"},
		},
		Groups: map[string]ModifierGroup{
			"sugar": {ID: "sugar", Name: "Sugar", NameAR: "السكر", Modifiers: []Modifier{
				{ID: "sugar-normal", Name: "Normal sugar", NameAR: "سكر عادي", Default: true},
				{ID: "sugar-less", Name: "Less sugar", NameAR: "سكر قليل"},
				{ID: "sugar-none", Name: "No sugar", NameAR: "بدون سكر"},
				{ID: "sugar-extra", Name: "Extra sugar", NameAR: "سكر زيادة"},
			}},
			"spice": {ID: "spice", Name: "Spice", NameAR: "الحرارة", Modifiers: []Modifier{
				{ID: "spice-normal", Name: "Normal spice", NameAR: "عادي", Default: true},
				{ID: "spice-mild", Name: "Mild", NameAR: "خفيف"},
				{ID: "spice-hot", Name: "Extra spicy", NameAR: "حار جداً"},
			}},
			"extras": {ID: "extras", Name: "Extras", NameAR: "إضافات", Multiple: true, Modifiers: []Modifier{
				{ID: "extra-parotta", Name: "Extra parotta", NameAR: "براتا إضافية", Delta: 100},
				{ID: "extra-rice", Name: "Extra rice", NameAR: "رز إضافي", Delta: 300},
				{ID: "extra-gravy", Name: "Extra gravy", NameAR: "مرق إضافي", Delta: 200},
			}},
		},
	}
	for i := range c.Categories {
		c.Categories[i].Position = i
	}
	for id, g := range c.Groups {
		for i := range g.Modifiers {
			g.Modifiers[i].GroupID, g.Modifiers[i].Position = id, i
		}
		c.Groups[id] = g
	}
	add := func(id, cat, name, ar string, price money.Fils, groups ...string) {
		kitchen := false
		if k, ok := c.Category(cat); ok {
			kitchen = k.Kitchen
		}
		c.Items = append(c.Items, Item{ID: id, CategoryID: cat, Name: name, NameAR: ar, Price: price,
			Position: len(c.Items), Kitchen: kitchen, Groups: groups})
	}
	add("karak", "tea", "Karak chai", "شاي كرك", 100, "sugar")
	add("karak-saffron", "tea", "Saffron karak", "كرك زعفران", 200, "sugar")
	add("karak-ginger", "tea", "Ginger karak", "كرك زنجبيل", 150, "sugar")
	add("masala-chai", "tea", "Masala chai", "شاي ماسالا", 150, "sugar")
	add("sulaimani", "tea", "Sulaimani", "شاي سليماني", 100, "sugar")
	add("lemon-tea", "tea", "Lemon tea", "شاي ليمون", 150, "sugar")
	add("green-tea", "tea", "Green tea", "شاي أخضر", 200, "sugar")
	add("nescafe", "tea", "Nescafe", "نسكافيه", 200, "sugar")
	add("karak-large", "tea", "Karak, large", "كرك كبير", 200, "sugar")

	add("biryani-chicken", "meals", "Chicken biryani", "برياني دجاج", 1400, "spice", "extras")
	add("biryani-mutton", "meals", "Mutton biryani", "برياني لحم", 1800, "spice", "extras")
	add("mandi-chicken", "meals", "Chicken mandi", "مندي دجاج", 1800, "extras")
	add("beef-fry-parotta", "meals", "Beef fry and parotta", "لحم بقر مقلي مع براتا", 1200, "spice", "extras")
	add("chicken-curry-rice", "meals", "Chicken curry and rice", "كاري دجاج مع رز", 1200, "spice", "extras")
	add("fish-curry-meals", "meals", "Fish curry meals", "وجبة كاري سمك", 1400, "spice", "extras")
	add("veg-meals", "meals", "Kerala meals, veg", "وجبة نباتية", 1000, "extras")
	add("dal-chapati", "meals", "Dal fry and chapati", "دال مع شباتي", 700, "spice")
	add("egg-curry-parotta", "meals", "Egg curry and parotta", "كاري بيض مع براتا", 800, "spice", "extras")
	add("chicken-65", "meals", "Chicken 65", "دجاج 65", 1200, "spice")
	add("chilli-chicken", "meals", "Chilli chicken", "دجاج بالفلفل الحار", 1300, "spice")
	add("fried-rice-chicken", "meals", "Chicken fried rice", "رز مقلي بالدجاج", 1200, "spice")
	add("noodles-chicken", "meals", "Chicken noodles", "نودلز بالدجاج", 1200, "spice")
	add("shawarma-plate", "meals", "Shawarma plate", "صحن شاورما", 1300)

	add("parotta", "snacks", "Parotta", "براتا", 100)
	add("chapati", "snacks", "Chapati", "شباتي", 100)
	add("samosa", "snacks", "Samosa, 3 pieces", "سمبوسة، ٣ قطع", 200)
	add("shawarma-chicken", "snacks", "Chicken shawarma", "شاورما دجاج", 600, "spice")
	add("falafel-sandwich", "snacks", "Falafel sandwich", "ساندويتش فلافل", 400)
	add("egg-sandwich", "snacks", "Egg sandwich", "ساندويتش بيض", 400)
	add("cheese-paratha", "snacks", "Cheese paratha", "براتا بالجبن", 500)
	add("zaatar-manakish", "snacks", "Zaatar manakish", "مناقيش زعتر", 400)

	add("water-small", "drinks", "Water, small", "ماء صغير", 100)
	add("laban", "drinks", "Laban", "لبن", 250)
	add("soft-drink", "drinks", "Soft drink, can", "مشروب غازي", 250)
	add("fresh-lime", "drinks", "Fresh lime", "ليمون طازج", 500, "sugar")
	add("mango-juice", "drinks", "Mango juice", "عصير مانجو", 800)
	add("avocado-juice", "drinks", "Avocado juice", "عصير أفوكادو", 1200)
	return c
}
