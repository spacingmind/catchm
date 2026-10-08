/**
 * An illustration of the see-through subtitle mode: captions in the
 * overlay's style (white, black outline) floating over a video call with
 * a shared slide. The call is a drawing, the captions are real text.
 */
export function SubtitleScene() {
  const bars = [38, 56, 44, 72, 64, 88];
  const people = [
    { initial: "A", tint: "#8a5a45" },
    { initial: "B", tint: "#6f7a68" },
  ];
  return (
    <div
      role="img"
      aria-label="Illustration: a video call with a shared slide and two participants. Captions float over it: Let's review the roadmap for next quarter, with its Vietnamese translation underneath."
      className="relative aspect-[16/10] overflow-hidden rounded-xl bg-[#231a16]"
    >
      <div aria-hidden="true" className="absolute inset-2.5 grid grid-cols-[1fr_22%] gap-2 sm:inset-3 sm:gap-2.5">
        <div className="rounded-lg bg-gradient-to-br from-[#3c2e28] to-[#2a201b] p-3 sm:p-4">
          <div className="h-2.5 w-2/5 rounded-full bg-white/30" />
          <div className="mt-2 h-2 w-1/4 rounded-full bg-white/15" />
          <div className="mt-4 flex h-[44%] items-end gap-[3.5%] sm:mt-6">
            {bars.map((h, i) => (
              <div
                key={i}
                className="w-[11%] rounded-t-md bg-[#d9c4b4]/45"
                style={{ height: `${h}%` }}
              />
            ))}
          </div>
        </div>
        <div className="flex flex-col gap-2 sm:gap-2.5">
          {people.map((p) => (
            <div
              key={p.initial}
              className="grid flex-1 place-items-center rounded-lg bg-gradient-to-b from-[#3a2d27] to-[#2c211c]"
            >
              <span
                className="grid size-8 place-items-center rounded-full text-[0.7rem] font-semibold text-white/90 sm:size-11 sm:text-sm"
                style={{ background: p.tint }}
              >
                {p.initial}
              </span>
            </div>
          ))}
        </div>
      </div>
      <div
        aria-hidden="true"
        className="subtitle absolute inset-x-0 bottom-3.5 px-4 text-center text-[clamp(0.78rem,2.3vw,1.02rem)] leading-snug sm:bottom-5"
      >
        <p className="caption-in" style={{ "--d": "300ms" } as React.CSSProperties}>
          Let&rsquo;s review the roadmap for next quarter.
        </p>
        <p className="caption-in" style={{ "--d": "700ms" } as React.CSSProperties}>
          Hãy xem lại lộ trình cho quý tới.
        </p>
      </div>
    </div>
  );
}
