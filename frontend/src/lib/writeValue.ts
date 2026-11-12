// Typed write values for the tag write control (issue #61).
//
// The write endpoint accepts only native JSON values matching the tag's
// declared Sparkplug type (strict JSON: strings are never parsed server-side).
// These helpers turn the raw input text into that typed value and describe the
// input to render for each type. The server stays authoritative: anything that
// passes here is still validated and may come back as 400 invalid_value.

export type WriteValueResult =
  | { ok: true; value: boolean | number | string }
  | { ok: false; error: string };

interface IntRange {
  min: bigint;
  max: bigint;
}

const INT_RANGES: Record<string, IntRange> = {
  Int8: { min: -128n, max: 127n },
  Int16: { min: -32768n, max: 32767n },
  Int32: { min: -2147483648n, max: 2147483647n },
  Int64: { min: -9223372036854775808n, max: 9223372036854775807n },
  UInt8: { min: 0n, max: 255n },
  UInt16: { min: 0n, max: 65535n },
  UInt32: { min: 0n, max: 4294967295n },
  UInt64: { min: 0n, max: 18446744073709551615n },
};

const MAX_FLOAT32 = 3.4028234663852886e38;
const MAX_SAFE = BigInt(Number.MAX_SAFE_INTEGER);

export function isIntegerType(type: string): boolean {
  return type in INT_RANGES;
}

export function isFloatType(type: string): boolean {
  return type === "Float" || type === "Double";
}

/** Attributes for the <input> rendered for a non-Boolean tag type. */
export function inputPropsFor(type: string): {
  type: "number" | "text";
  step?: string;
  min?: string;
  max?: string;
  inputMode?: "numeric" | "decimal";
} {
  const range = INT_RANGES[type];
  if (range) {
    // min/max only where a JS number represents the bound exactly.
    const exact = range.min >= -MAX_SAFE && range.max <= MAX_SAFE;
    return {
      type: "number",
      step: "1",
      inputMode: "numeric",
      ...(exact ? { min: range.min.toString(), max: range.max.toString() } : {}),
    };
  }
  if (isFloatType(type)) {
    return { type: "number", step: "any", inputMode: "decimal" };
  }
  return { type: "text" };
}

/**
 * Converts the raw input text into the JSON value for the tag's declared type.
 * Boolean tags use a checkbox and pass `checked` directly, not this function.
 */
export function parseWriteValue(raw: string, type: string): WriteValueResult {
  if (type === "String") {
    return { ok: true, value: raw };
  }
  const text = raw.trim();
  const range = INT_RANGES[type];
  if (range) {
    const hint = `${type} expects an integer between ${range.min} and ${range.max}`;
    if (!/^-?\d+$/.test(text)) {
      return { ok: false, error: hint };
    }
    const n = BigInt(text);
    if (n < range.min || n > range.max) {
      return { ok: false, error: hint };
    }
    if (n < -MAX_SAFE || n > MAX_SAFE) {
      // JSON.stringify would round the number; refuse rather than write a
      // different value than the one typed.
      return {
        ok: false,
        error: `integers beyond ±${Number.MAX_SAFE_INTEGER} cannot be written from the UI`,
      };
    }
    return { ok: true, value: Number(n) };
  }
  if (isFloatType(type)) {
    const f = text === "" ? NaN : Number(text);
    if (!Number.isFinite(f)) {
      return { ok: false, error: `${type} expects a number` };
    }
    if (type === "Float" && Math.abs(f) > MAX_FLOAT32) {
      return { ok: false, error: "Float expects a number within the float32 range" };
    }
    return { ok: true, value: f };
  }
  return { ok: false, error: `unsupported tag type ${JSON.stringify(type)}` };
}
