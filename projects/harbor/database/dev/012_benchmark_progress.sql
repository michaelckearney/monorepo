-- A trial shows on the Benchmarks page as it goes, not only once it ends.
-- Its chat is in the trial's own database until then, which only its run's
-- process can read, so the run copies it here as it changes.
--
-- started_at is when the agent's turn started. transcript is the chat so
-- far, as JSON, while the trial goes, and where a trial cancelled or
-- stopped got to; a done trial's is in result. input_tokens and
-- output_tokens count up meanwhile.
ALTER TABLE benchmark_trials ADD COLUMN started_at TEXT;
ALTER TABLE benchmark_trials ADD COLUMN transcript TEXT
    CHECK (transcript IS NULL OR json_valid(transcript));
