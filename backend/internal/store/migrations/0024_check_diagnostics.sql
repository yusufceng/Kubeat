-- Why a failed check failed (store.Diagnostics) as compact JSON: what the
-- failure most likely means and, for HTTP(S), where the time went and who
-- answered how; never anything of the answer's body. NULL for every
-- successful check and for a failure with nothing to add. It is deleted
-- with its result, so it is kept as long as the raw results are.
ALTER TABLE check_results ADD COLUMN diagnostics TEXT;
