import { Link } from "@tanstack/react-router";
import { EmptyState, Button } from "@/components/ui";

export function NotFound() {
  return (
    <EmptyState
      title="Page not found"
      body="The page you are looking for does not exist, or you do not have access to it."
      action={
        <Link to="/projects">
          <Button>Go to projects</Button>
        </Link>
      }
    />
  );
}
