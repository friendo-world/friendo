package deploy

import "testing"

func TestReplaceContentBlock(t *testing.T) {
	block := "[content]\ncollections = [\"blog\", \"recipes\"]\n\n[content.recipes.fields]\nserves = \"number\"\n"

	t.Run("replaces the section and its tables, keeping the rest", func(t *testing.T) {
		in := `[site]
name = "x"

[content]
collections = ["blog"]

[content.blog.fields]
tags = "tags"

# Optional: settings
[settings]
comments_need_review = false
`
		want := `[site]
name = "x"

[content]
collections = ["blog", "recipes"]

[content.recipes.fields]
serves = "number"

# Optional: settings
[settings]
comments_need_review = false
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
		in := "[site]\nname = \"x\"\n\n[content]\ncollections = [\"blog\"]\n"
		want := "[site]\nname = \"x\"\n\n" + block
		if got := replaceContentBlock(in, block); got != want {
			t.Fatalf("got\n%s\nwant\n%s", got, want)
		}
	})
}
