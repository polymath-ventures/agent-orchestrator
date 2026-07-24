import { createFileRoute } from "@tanstack/react-router";
import { PrimeBoard } from "../components/PrimeBoard";

export const Route = createFileRoute("/_shell/prime")({
	component: PrimeBoard,
});
