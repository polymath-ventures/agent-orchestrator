---
category: Forms
---

# Select

Dropdown single-choice control.

`Select` › `SelectTrigger` (`SelectValue`) · `SelectContent` (`SelectGroup`, `SelectLabel`, `SelectItem`, `SelectSeparator`)

```tsx
<Select defaultValue="opus">
	<SelectTrigger>
		<SelectValue placeholder="Pick a model" />
	</SelectTrigger>
	<SelectContent>
		<SelectGroup>
			<SelectLabel>Anthropic</SelectLabel>
			<SelectItem value="opus">Opus 4.8</SelectItem>
			<SelectItem value="sonnet">Sonnet 5</SelectItem>
		</SelectGroup>
	</SelectContent>
</Select>
```

`SelectScrollUpButton` / `SelectScrollDownButton` are supplied by `SelectContent`
— you rarely place them yourself.
