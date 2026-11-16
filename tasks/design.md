# The design task

Propose one UI improvement at a time, as an issue a bot then works like any
other. You are the design agent: one run, one proposal at most. Opening
nothing is a fine outcome.

## Each run

1. Read [framework/DESIGN.md](../framework/DESIGN.md), the design every app
   shares. Its [Proposals](../framework/DESIGN.md#proposals) section is the
   rules for a proposal: follow it, don't restate it. Then look through the
   gallery the `harness test -e2e` that ran before you has just made:
   `projects/<name>/.desktop/gallery/<mode>/<TestName>.png`, one screenshot
   per e2e test and subtest, each subtest separator an underscore. Look at
   every picture, in every project and mode, before choosing. The gallery
   is light mode only, the scheme every screenshot is pinned to.
2. Pick one problem on one screen: something you can see in a screenshot,
   not a general wish about an app. Judge only what the pictures show. When
   the problem is in a flow rather than a picture — focus, the keyboard, the
   steps through something, what survives a restart — drive the app with
   `harness record -p <project>` and judge its screenshots; the framework
   README's `harness record` section has the commands.
3. Read the past `design` issues, open and closed, and the owner's reasons
   on the closed ones. Propose nothing one of them already covers, unless
   you say what is different now.
4. Open one issue with `harness open-issue -label project:<name> -` (or
   `-label framework` for one change to the shared `framework/ui`); the
   harness adds the `design` label and uploads the images you embed. Say in
   it:

   - the problem, and what DESIGN.md asks for that the screen fails;
   - the "before" screenshots, shown as Markdown images with the path from
     the monorepo's root: from the gallery
     (`projects/<name>/.desktop/gallery/...`), or from a recording
     (`projects/<name>/.desktop/recordings/...`) when you drove a flow;
   - a spec precise enough to build: which component, which tokens, what is
     removed;
   - what done looks like, including before and after screenshots in the
     pull request;
   - the related past proposals, by number.

5. Open no issue when you find nothing worth proposing, and say so. Do not
   stretch a screenshot into a proposal just to have one.
6. When the owner's reason on a closed proposal states a rule DESIGN.md
   lacks, you may instead propose adding that rule to DESIGN.md, with scope
   `framework`.

## After you

You open at most one issue a run, as the owner, with exactly one scope
label; the harness adds `design`. The issue's implementation is a normal bot
thread: once opened, you take no part in it. You cannot comment, label, or
reply.
