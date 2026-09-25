import { label } from "./lib";

export function render(scores: number[]): string[] {
  return scores.map((s) => label(s));
}
