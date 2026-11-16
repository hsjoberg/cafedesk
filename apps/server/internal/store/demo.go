package store

import (
	"context"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
)

// SeedDemoMenu adds the demo menu the till starts with, so a new server has
// something to edit: tea, meals, snacks and drinks, with sugar, spice and
// extras. Prices and Arabic names are examples.
func (s *Store) SeedDemoMenu(ctx context.Context, orgID uuid.UUID) error {
	ar := func(s string) *string { return &s }
	group := func(name, nameAr string, multiple bool, mods ...api.ModifierInput) (uuid.UUID, error) {
		g, err := s.CreateModifierGroup(ctx, orgID, api.ModifierGroupInput{Name: name, NameAr: ar(nameAr),
			Multiple: multiple, Modifiers: mods})
		return g.Id, err
	}
	mod := func(name, nameAr string, delta int64, def bool) api.ModifierInput {
		return api.ModifierInput{Name: name, NameAr: ar(nameAr), PriceDeltaFils: delta, IsDefault: def}
	}
	sugar, err := group("Sugar", "السكر", false, mod("Normal sugar", "سكر عادي", 0, true),
		mod("Less sugar", "سكر قليل", 0, false), mod("No sugar", "بدون سكر", 0, false), mod("Extra sugar", "سكر زيادة", 0, false))
	if err != nil {
		return err
	}
	spice, err := group("Spice", "الحرارة", false, mod("Normal spice", "عادي", 0, true),
		mod("Mild", "خفيف", 0, false), mod("Extra spicy", "حار جداً", 0, false))
	if err != nil {
		return err
	}
	extras, err := group("Extras", "إضافات", true, mod("Extra parotta", "براتا إضافية", 100, false),
		mod("Extra rice", "رز إضافي", 300, false), mod("Extra gravy", "مرق إضافي", 200, false))
	if err != nil {
		return err
	}

	type item struct {
		name, nameAr string
		price        int64
		groups       []uuid.UUID
	}
	categories := []struct {
		name, nameAr, color string
		kitchen             bool
		items               []item
	}{
		{"Tea", "شاي", "#5F8A3A", false, []item{
			{"Karak chai", "شاي كرك", 100, []uuid.UUID{sugar}},
			{"Saffron karak", "كرك زعفران", 200, []uuid.UUID{sugar}},
			{"Ginger karak", "كرك زنجبيل", 150, []uuid.UUID{sugar}},
			{"Masala chai", "شاي ماسالا", 150, []uuid.UUID{sugar}},
			{"Sulaimani", "شاي سليماني", 100, []uuid.UUID{sugar}},
			{"Lemon tea", "شاي ليمون", 150, []uuid.UUID{sugar}},
			{"Green tea", "شاي أخضر", 200, []uuid.UUID{sugar}},
			{"Nescafe", "نسكافيه", 200, []uuid.UUID{sugar}},
			{"Karak, large", "كرك كبير", 200, []uuid.UUID{sugar}},
		}},
		{"Meals", "وجبات", "#2F6690", true, []item{
			{"Chicken biryani", "برياني دجاج", 1400, []uuid.UUID{spice, extras}},
			{"Mutton biryani", "برياني لحم", 1800, []uuid.UUID{spice, extras}},
			{"Chicken mandi", "مندي دجاج", 1800, []uuid.UUID{extras}},
			{"Beef fry and parotta", "لحم بقر مقلي مع براتا", 1200, []uuid.UUID{spice, extras}},
			{"Chicken curry and rice", "كاري دجاج مع رز", 1200, []uuid.UUID{spice, extras}},
			{"Fish curry meals", "وجبة كاري سمك", 1400, []uuid.UUID{spice, extras}},
			{"Kerala meals, veg", "وجبة نباتية", 1000, []uuid.UUID{extras}},
			{"Dal fry and chapati", "دال مع شباتي", 700, []uuid.UUID{spice}},
			{"Egg curry and parotta", "كاري بيض مع براتا", 800, []uuid.UUID{spice, extras}},
			{"Chicken 65", "دجاج 65", 1200, []uuid.UUID{spice}},
			{"Chilli chicken", "دجاج بالفلفل الحار", 1300, []uuid.UUID{spice}},
			{"Chicken fried rice", "رز مقلي بالدجاج", 1200, []uuid.UUID{spice}},
			{"Chicken noodles", "نودلز بالدجاج", 1200, []uuid.UUID{spice}},
			{"Shawarma plate", "صحن شاورما", 1300, nil},
		}},
		{"Snacks", "وجبات خفيفة", "#7B4B94", true, []item{
			{"Parotta", "براتا", 100, nil},
			{"Chapati", "شباتي", 100, nil},
			{"Samosa, 3 pieces", "سمبوسة، ٣ قطع", 200, nil},
			{"Chicken shawarma", "شاورما دجاج", 600, []uuid.UUID{spice}},
			{"Falafel sandwich", "ساندويتش فلافل", 400, nil},
			{"Egg sandwich", "ساندويتش بيض", 400, nil},
			{"Cheese paratha", "براتا بالجبن", 500, nil},
			{"Zaatar manakish", "مناقيش زعتر", 400, nil},
		}},
		{"Drinks", "مشروبات", "#1E8A83", false, []item{
			{"Water, small", "ماء صغير", 100, nil},
			{"Laban", "لبن", 250, nil},
			{"Soft drink, can", "مشروب غازي", 250, nil},
			{"Fresh lime", "ليمون طازج", 500, []uuid.UUID{sugar}},
			{"Mango juice", "عصير مانجو", 800, nil},
			{"Avocado juice", "عصير أفوكادو", 1200, nil},
		}},
	}
	for _, c := range categories {
		cat, err := s.CreateCategory(ctx, orgID, api.CategoryInput{Name: c.name, NameAr: ar(c.nameAr), Color: c.color, Kitchen: c.kitchen})
		if err != nil {
			return err
		}
		for _, it := range c.items {
			groups := it.groups
			if groups == nil {
				groups = []uuid.UUID{}
			}
			if _, err := s.CreateItem(ctx, orgID, api.ItemInput{CategoryId: cat.Id, Name: it.name, NameAr: ar(it.nameAr),
				PriceFils: it.price, Kitchen: c.kitchen, ModifierGroupIds: groups}); err != nil {
				return err
			}
		}
	}
	return nil
}
