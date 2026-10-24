import { label } from "../../web/lib";

export function Scores({ scores }: { scores: number[] }) {
  return (
    <ul>
      {scores.map((s) => (
        <li key={s}>{s > 0 ? label(s) : "none"}</li>
      ))}
    </ul>
  );
}
