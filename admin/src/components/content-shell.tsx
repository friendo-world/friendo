import type { ComponentChildren } from "preact";
import { useEffect, useState } from "preact/hooks";
import { api, type Collection } from "../api";
import { CollectionSidebar } from "./collection-sidebar";

// The content area's frame: the collections sidebar beside whatever the page is
// showing. A page that already holds the collections passes them in (the
// collections view adjusts their counts as records come and go); otherwise the
// shell loads them.
export function ContentShell({
  collection,
  collections: given,
  declared: givenDeclared,
  children,
}: {
  collection?: string;
  collections?: Collection[] | null;
  declared?: boolean;
  children: ComponentChildren;
}) {
  const [own, setOwn] = useState<Collection[] | null>(null);
  const [ownDeclared, setOwnDeclared] = useState(false);
  const loadOwn = given === undefined;

  useEffect(() => {
    if (!loadOwn) return;
    api
      .collections()
      .then((r) => {
        setOwn(r.collections);
        setOwnDeclared(!!r.declared);
      })
      .catch(() => setOwn([]));
  }, [loadOwn]);

  return (
    <div class="mx-auto flex max-w-[1200px] flex-col md:flex-row">
      <CollectionSidebar
        collections={loadOwn ? own : given}
        declared={loadOwn ? ownDeclared : !!givenDeclared}
        active={collection || ""}
      />
      <main class="min-w-0 flex-1 px-4 py-6">{children}</main>
    </div>
  );
}
