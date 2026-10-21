"use client";

import { useState } from "react";
import { type DurationUnit, durationUnits, joinSeconds, splitSeconds } from "@/lib/duration-input";
import { useT } from "@/lib/i18n/client";

/**
 * A length of time as a count and a unit side by side ("10 | minutes"),
 * sent to the form as whole seconds under `name`. A stored value opens in
 * the largest unit that divides it. onChange gets the seconds as they are
 * typed, or null while the count is not a whole number, so that the help
 * beside the field can read the value back.
 */
export function DurationField({
  name,
  label,
  seconds,
  onChange,
}: {
  name: string;
  /** The field's name for screen readers; the unit's is derived from it. */
  label: string;
  /** The stored value, in seconds. */
  seconds: number;
  onChange: (seconds: number | null) => void;
}) {
  const t = useT();
  const initial = splitSeconds(seconds);
  const [count, setCount] = useState(String(initial.count));
  const [unit, setUnit] = useState<DurationUnit>(initial.unit);
  const total = joinSeconds(count, unit);

  function update(nextCount: string, nextUnit: DurationUnit) {
    setCount(nextCount);
    setUnit(nextUnit);
    onChange(joinSeconds(nextCount, nextUnit));
  }

  return (
    <span className="al-duration">
      <input
        type="number"
        inputMode="numeric"
        min={0}
        step={1}
        required
        value={count}
        onChange={(e) => update(e.target.value, unit)}
        aria-label={label}
        className="field"
      />
      <select
        value={unit}
        onChange={(e) => update(count, e.target.value as DurationUnit)}
        aria-label={t("duration.unitOf", { label })}
        className="field"
      >
        {durationUnits.map((u) => (
          <option key={u} value={u}>
            {t(`duration.unit.${u}`)}
          </option>
        ))}
      </select>
      {/* What the form sends: the seconds, or nothing while the count is not a number. */}
      <input type="hidden" name={name} value={total ?? ""} />
    </span>
  );
}
