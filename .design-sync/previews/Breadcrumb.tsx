import { Breadcrumb, BreadcrumbItem, BreadcrumbList, BreadcrumbPage, BreadcrumbSeparator } from "agent-orchestrator";

export function ProjectPath() {
	return (
		<Breadcrumb>
			<BreadcrumbList>
				<BreadcrumbItem>Projects</BreadcrumbItem>
				<BreadcrumbSeparator />
				<BreadcrumbItem>agent-orchestrator</BreadcrumbItem>
				<BreadcrumbSeparator />
				<BreadcrumbItem>
					<BreadcrumbPage>fix-daemon-restart-race</BreadcrumbPage>
				</BreadcrumbItem>
			</BreadcrumbList>
		</Breadcrumb>
	);
}

export function DeepSessionPath() {
	return (
		<Breadcrumb>
			<BreadcrumbList>
				<BreadcrumbItem>Projects</BreadcrumbItem>
				<BreadcrumbSeparator />
				<BreadcrumbItem>agent-orchestrator</BreadcrumbItem>
				<BreadcrumbSeparator />
				<BreadcrumbItem>Sessions</BreadcrumbItem>
				<BreadcrumbSeparator />
				<BreadcrumbItem>worker-7</BreadcrumbItem>
				<BreadcrumbSeparator />
				<BreadcrumbItem>
					<BreadcrumbPage>review</BreadcrumbPage>
				</BreadcrumbItem>
			</BreadcrumbList>
		</Breadcrumb>
	);
}

export function TwoLevel() {
	return (
		<Breadcrumb>
			<BreadcrumbList>
				<BreadcrumbItem>Projects</BreadcrumbItem>
				<BreadcrumbSeparator />
				<BreadcrumbItem>
					<BreadcrumbPage>web-supervisor</BreadcrumbPage>
				</BreadcrumbItem>
			</BreadcrumbList>
		</Breadcrumb>
	);
}
