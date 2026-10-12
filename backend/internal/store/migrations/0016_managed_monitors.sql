-- Monitors that discovery keeps in step with annotated cluster objects.
ALTER TABLE monitors ADD COLUMN managed INTEGER NOT NULL DEFAULT 0;
