/** 単位数の表示（2 → "2"、1.5 → "1.5"）。 */
export const credits = (n: number | undefined) => String(Math.round((n ?? 0) * 10) / 10)
