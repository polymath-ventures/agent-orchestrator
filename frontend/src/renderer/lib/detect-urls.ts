// Watches a terminal output stream for http(s) URLs and reports each new one
// exactly once. Used to "glow" the Browser tab when an agent prints a link
// without the user having to click it.

const ANSI_PATTERN = new RegExp("[\\u001B\\u009B][[\\]()#;?]*(?:[0-9]{1,4}(?:;[0-9]{0,4})*)?[0-9A-ORZcf-nqry=><]", "g");
const URL_PATTERN = /\bhttps?:\/\/[^\s"'`<>()[\]{}]+/gi;
const TRAILING_PUNCT = /[.,;:!?)\]}'"]+$/;

const TAIL_MAX = 2048;
const SEEN_MAX = 512;

export type UrlWatcher = {
	push: (chunk: string) => void;
	reset: () => void;
};

function stripTrailingPunct(url: string): string {
	return url.replace(TRAILING_PUNCT, "");
}

export function isWebLink(uri: string): boolean {
	try {
		const { protocol } = new URL(uri);
		return protocol === "http:" || protocol === "https:";
	} catch {
		return false;
	}
}

export function createUrlWatcher(onUrl: (url: string) => void): UrlWatcher {
	let tail = "";
	let seen = new Set<string>();

	return {
		push(chunk: string) {
			if (!chunk) return;
			const text = (tail + chunk).replace(ANSI_PATTERN, "");
			URL_PATTERN.lastIndex = 0;
			let match: RegExpExecArray | null;
			while ((match = URL_PATTERN.exec(text)) !== null) {
				const end = match.index + match[0].length;
				if (end === text.length) break;
				const url = stripTrailingPunct(match[0]);
				if (!url || seen.has(url)) continue;
				if (seen.size >= SEEN_MAX) seen = new Set();
				seen.add(url);
				onUrl(url);
			}
			tail = text.length > TAIL_MAX ? text.slice(text.length - TAIL_MAX) : text;
		},
		reset() {
			tail = "";
			seen = new Set();
		},
	};
}
