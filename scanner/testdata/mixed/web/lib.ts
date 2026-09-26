export function label(score: number): string {
  if (score >= 90) return "A";
  return score >= 80 ? "B" : "C";
}
