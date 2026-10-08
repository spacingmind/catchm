import { EngineList } from "@/components/engine-chips";
import { Frame, Pill, Row, SectionHead } from "@/components/feature-row";
import { SubtitleScene } from "@/components/subtitle-scene";
import { Shot } from "@/components/ui/shot";

export function Live() {
  return (
    <section id="live" aria-labelledby="live-title" className="border-t border-line bg-bg-alt">
      <div className="mx-auto max-w-6xl px-5 py-20 sm:px-8 sm:py-28">
        <SectionHead id="live-title" eyebrow="During the meeting" title="Follow along in any language" />

        <div className="mt-16 space-y-20 sm:space-y-28">
          <Row
            title="Live transcript, live translation"
            body="Each line shows up as it's said, with its translation underneath. Two-way mode for bilingual meetings."
            visual={
              <div className="space-y-4">
                <Frame>
                  <Shot
                    name="transcript"
                    alt="A live transcript with each line in its original language and its translation underneath. Buttons switch between original plus translation, side by side, and translation only."
                    width={848}
                    height={284}
                    sizes="(min-width: 1152px) 600px, 90vw"
                  />
                </Frame>
                <Frame className="ml-6 sm:ml-14">
                  <Shot
                    name="sidebyside"
                    alt="The same transcript in side-by-side layout, original on the left and translation on the right."
                    width={848}
                    height={212}
                    sizes="(min-width: 1152px) 540px, 80vw"
                  />
                </Frame>
              </div>
            }
          />

          <Row
            flip
            title="A subtitle window for your call"
            body="A small always-on-top overlay. Make it see-through and it floats over your call like captions."
            note="See-through mode works on macOS today. Windows 11 is in testing."
            visual={
              <div>
                <div className="relative">
                  <SubtitleScene />
                  <div className="absolute top-3 left-3">
                    <Pill>See-through</Pill>
                  </div>
                </div>
                <figure className="mt-4 ml-auto w-[62%] sm:w-[48%]">
                  <Frame className="rounded-lg">
                    <Shot
                      name="overlay"
                      alt="The compact overlay in its solid style: a small window with a live transcript and translation."
                      width={480}
                      height={260}
                      sizes="(min-width: 640px) 280px, 62vw"
                    />
                  </Frame>
                  <figcaption className="mt-3">
                    <Pill>Solid</Pill>
                  </figcaption>
                </figure>
              </div>
            }
          />
        </div>

        <div className="mt-16 grid gap-5 sm:mt-20 md:grid-cols-2">
          <EngineList
            title="Speech to text"
            items={[
              ["Soniox", "cloud", "Labels who's speaking and translates as it goes. Bring your own key."],
              ["Whisper", "local", "Runs through the whisper.cpp server."],
            ]}
          />
          <EngineList
            title="Translation"
            items={[
              ["Soniox", "cloud", "Built in, live, one-way or two-way."],
              ["Ollama", "local", "A local translation model."],
            ]}
          />
        </div>
      </div>
    </section>
  );
}
