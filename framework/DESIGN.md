# Design

Utilitarian, but not hostile. Dense, but easy to read. Intentional, and our own.

These principles hold for every app in this monorepo. They are defaults, not
laws: a proposal may argue against one, with evidence. Each has a reason; when
a case isn't covered, follow the reason. Examples show a principle; they don't
extend it. A project's own examples are in its AGENTS.md.

## Every element earns its place

Text, icons, colour, and controls are there to tell someone something or let
them do something. What only decorates, repeats, or explains how the app works
inside is noise, and noise hides what matters.

- Label things; when something fails, say what's wrong and what to do.
- No intro paragraphs, disclaimers, or subtitles that restate the title.
  People can read a lock icon.
- UI text is for someone using the app, not someone building it.
- An icon, dot, or badge must differ from its neighbours or report a state.
  The same icon on every row says nothing.
- Plain beats ornamented: a plain icon reads cleaner than one in a frame.
- Of two solutions, prefer the one with fewer parts. Never remove a capability
  as a styling change.

## Signal what needs attention

Attention is the scarce resource. Colour and emphasis go to what someone is
waiting on or must act on, never to the background normal, so the signal that
matters stands out.

- Amber: needs you. Red: failed, or about to be destroyed. The accent: you can
  act on this.
- Red appears at the point of destruction (the confirm), not on every delete
  button at rest.
- Show status on the thing it describes, plus one summary where people look
  first.
- Never rely on colour alone; pair it with a shape, icon, or word.

## Hierarchy comes from space and type

Structure should come from spacing, type scale, and weight before lines and
boxes. Every line competes with every other; boxes inside boxes read as a form,
not a product.

- Separate regions by tone and space before borders.
- A surface gets a fill or a border, rarely both.
- Shadows are for things floating above the page.
- Dense lists and tables may use dividers; contrast-poor themes may need more.

## Stable and predictable

Things stay where they are, so people can learn where they are. Predictable
beats clever.

- A component keeps its size whether it's loading, empty, signed out, or full.
- Controls reflect state: disabled when there's nothing to do, enabled when
  there is, hidden only when they can never apply here.
- Selecting something may preview it; nothing is committed until it's saved,
  and Cancel reverts.
- Don't fix layout with automatic guessing; give the author, person or agent,
  better guardrails.

## Behaves natively, looks like ours

The apps should behave like the platform and look like themselves.

- Focus, keyboard, cursors, and standard shortcuts work as the platform's do.
- Settings, menus, and dialogs follow the conventions people already know.
- The look comes from our tokens and themes, not a library's defaults.

## Accessible by default

Everything works from the keyboard, with focus that can be seen. Text meets
contrast in every theme. Icon-only controls have a label and a tooltip that
shows on focus as well as hover. Primary and destructive actions have text.

## One way to build each thing

Things that do the same job look and work the same, in every project. Use the
shared components and tokens in `framework/ui`. When the standard is wrong,
change the standard, for every app at once, rather than working around it in
one place.

## Mechanical rules

These are enforced by tests, not by judgement: sizes, spacing, radii, and
colours come from the tokens, with no arbitrary values. Harbor's
[tokens.test.ts](../projects/harbor/frontend/src/__tests__/tokens.test.ts)
checks this across its sources.

## Clichés to avoid

These make an app look like a template rather than something made on purpose:
the stock component-library look (default greys, default radius, identical 1px
rules everywhere), boxes inside boxes, a lone sentence centred in an empty
card, decorative icons in front of headings, gradients, glass, and glow.

## Proposals

- Start from a problem someone can see in a screenshot or a recording.
- Improve what's there in small steps; don't restyle the whole app at once.
- Show before and after.
- One problem per proposal. A UI change doesn't change backend logic; report a
  backend cause in the issue instead.
- Read past proposals and the reasons they were closed. A reason usually
  becomes an example here; a principle is added only when several examples
  point at one that's missing.
