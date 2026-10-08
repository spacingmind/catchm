import { Logo } from "@/components/logo";
import { ButtonLink } from "@/components/ui/button";

export default function NotFound() {
  return (
    <main className="mx-auto grid min-h-screen max-w-xl place-items-center px-6 text-center">
      <div>
        <Logo size={72} className="mx-auto" />
        <h1 className="display mt-8 text-5xl">Nothing caught here.</h1>
        <p className="mt-4 text-muted">That page doesn&rsquo;t exist.</p>
        <ButtonLink href="/" className="mt-8">
          Back to CatchM
        </ButtonLink>
      </div>
    </main>
  );
}
