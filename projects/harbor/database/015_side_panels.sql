-- The sidebar and the agent's panel are resized by dragging, and collapse to
-- a strip. settings gains sidebar_panel and agent_panel, each JSON:
-- {"width": pixels, "collapsed": true or false}. agent_panel was open, wide,
-- or closed, which the window no longer has.
DELETE FROM settings WHERE key = 'agent_panel';
