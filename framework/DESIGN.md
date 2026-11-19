# Design

Utilitarian, but not hostile. Dense, but easy to read. Calm, and our own.

These principles describe the direction every app in this monorepo shares.
They are a guide, not a rulebook. Each comes with its reason; when a case
isn't covered, or two principles pull different ways, follow the reasons and
choose the simpler, calmer screen.

## Less, and quieter

Every element should tell someone something or let them do something. What
only decorates, repeats, or explains how the app works inside is noise, and
noise hides what matters. Of two solutions, prefer the one with fewer parts;
a fix that adds more than it removes needs to be clearly worth it. Never
remove a capability to tidy a screen.

UI text is for someone using the app, not someone building it: label things,
and when something fails, say what's wrong and what to do.

## Attention goes where it's needed

Colour and emphasis are for what someone is waiting on or must act on, never
for the background normal, so the signal that matters stands out. Amber means
it needs you, red means it failed or is about to be destroyed, and the accent
means you can act on it. Use them sparingly: a screen full of signals has
none. Never rely on colour alone.

## Structure from space and type

Hierarchy comes from spacing, type size, and weight before lines and boxes.
Every line competes with every other, and boxes inside boxes read as a form,
not a product.

## Stable and predictable

Things stay where they are, so people can learn where they are. Components
keep their size whatever their state, controls reflect what can be done now,
and nothing is committed until it's saved. Predictable beats clever.

## Behaves natively, looks like ours

The apps behave like the platform, with its focus, keyboard, shortcuts, and
conventions for settings, menus, and dialogs, and look like themselves: the
look comes from our tokens and themes, not a component library's defaults.
Avoid what makes an app look like a template: stock library styling,
gradients, glass, and glow.

## Accessible by default

Everything works from the keyboard, with focus that can be seen. Text meets
contrast in every theme. Icon-only controls have a label and a tooltip.

## One way to build each thing

Things that do the same job look and work the same, in every project, using
the shared components and tokens in `framework/ui`. When the standard is
wrong, change it for every app at once rather than working around it in one
place.

## Mechanical rules

These are enforced by tests, not by judgement: sizes, spacing, radii, and
colours come from the tokens, with no arbitrary values. Harbor's
[tokens.test.ts](../projects/harbor/frontend/src/__tests__/tokens.test.ts)
checks this across its sources.

## Changing this document

Change it when a principle is missing or wrong, not to record each decision.
One decision about one screen is not a rule for every other.
