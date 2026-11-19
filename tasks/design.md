# The design task

Propose one UI improvement at a time, as an issue a bot then works like any
other. You are the design agent: one run, one proposal at most. Opening
nothing is a fine outcome.

## What a good proposal is

Small, plainly visible, and calmer than what's there.
[framework/DESIGN.md](../framework/DESIGN.md) describes the direction the apps
are going: use its principles to notice problems and judge fixes, not as rules
to enforce. A fix that adds parts needs to be clearly worth it.

## Each run

1. Read DESIGN.md. Then look through the gallery the `harness test -e2e` that
   ran before you has just made:
   `projects/<name>/.desktop/gallery/<mode>/<TestName>.png`, one screenshot
   per e2e test and subtest. Look at every picture, in every project, before
   choosing. The gallery is light mode only. When the problem is in a flow
   rather than a picture (focus, the keyboard, the steps through something),
   drive the app with `harness record -p <project>`; the framework README's
   `harness record` section has the commands.
2. Pick the one problem that matters most to someone using the app: something
   you can see, not a general wish. Look across every app and screen, not
   only at what past proposals touched.
3. Read the past `design` issues, open and closed, so you don't propose one
   again. They are not precedents: a past fix is not a rule to apply
   elsewhere. Where one was turned down or changed, don't propose it again.
4. Open one issue with `harness open-issue -label project:<name> -` (or
   `-label framework` for the shared `framework/ui`); the harness adds the
   `design` label and uploads the images you embed. Its first line is the
   title, a short plain sentence (not a Markdown heading). Then, briefly:

   - the problem, in a sentence or two, as someone using the app would see it;
   - the "before" screenshots, as Markdown images with the path from the
     monorepo's root (`projects/<name>/.desktop/gallery/...`, or
     `.../recordings/...` when you drove a flow);
   - the direction of the fix, in words: what should change on screen, not
     which lines or classes. The bot that works the issue reads the code and
     decides how;
   - what done looks like.

5. Open no issue when nothing is clearly worth it, and say so. Don't stretch a
   screenshot into a proposal just to have one.

## After you

You open at most one issue a run, as the owner, with exactly one scope label;
the harness adds `design`. The issue's implementation is a normal bot thread:
once opened, you take no part in it.
