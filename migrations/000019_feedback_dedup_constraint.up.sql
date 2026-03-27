-- Prevent duplicate feedback submissions at the DB level.
-- A session may only submit one signal per (session_id, model_slug, use_case) combination.
-- The application layer enforces a 1-hour window; this constraint is the
-- enforcement backstop that closes the check-then-insert race condition.
ALTER TABLE recommendation_feedback
  ADD CONSTRAINT uq_feedback_session_model_uc
    UNIQUE (session_id, model_slug, use_case);
