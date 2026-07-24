import { cn } from "@/lib/utils";

// bg-muted, not stock shadcn's bg-accent: `accent` is the brand blue here
// (--color-accent → --bridge-accent), so bg-accent turned every loading
// placeholder into solid blue bars. `muted` is the neutral surface role and
// tracks both themes. See DESIGN.md → Color.
function Skeleton({ className, ...props }: React.ComponentProps<"div">) {
	return <div data-slot="skeleton" className={cn("animate-pulse rounded-md bg-muted", className)} {...props} />;
}

export { Skeleton };
