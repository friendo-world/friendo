package deploy

import "testing"

func TestReplaceContentBlock(t *testing.T) {
	block := "[content]\ntypes = [\"blog\", \"recipes\"]\n\n[content.recipes.fields]\nserves = \"number\"\n"

	t.Run("replaces the section and its tables, keeping the rest", func(t *testing.T) {
		in := `[site]
name = "x"

[content]
types = ["blog"]

[content.blog.fields]
tags = "tags"

# Optional: settings
[settings]
auto_approve = true
`
		want := `[site]
name = "x"

[content]
types = ["blog", "recipes"]

[content.recipes.fields]
serves = "number"

# Optional: settings
[settings]
auto_approve = true
`
		if got := replaceContentBlock(in, block); got != want {
			t.Fatalf("got\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("appends when there is no [content]", func(t *testing.T) {
		in := "[site]\nname = \"x\"\n"
		want := "[site]\nname = \"x\"\n\n" + block
		if got := replaceContentBlock(in, block); got != want {
			t.Fatalf("got\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("replaces a trailing section", func(t *testing.T) {
		in := "[site]\nname = \"x\"\n\n[content]\ntypes = [\"blog\"]\n"
		want := "[site]\nname = \"x\"\n\n" + block
		if got := replaceContentBlock(in, block); got != want {
			t.Fatalf("got\n%s\nwant\n%s", got, want)
		}
	})
}
