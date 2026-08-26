import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, Loader2, Server, Sparkles } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { createEnvironment, type Environment } from "@/lib/environment-api";
import { cn } from "@/lib/utils";
import {
	parseCPULimitCores,
	parseMemoryLimitBytes,
} from "./environment-status-ring";

// Mirrors services/api's own validateCPULimit/validateMemoryLimit floors
// (environment_service/resource_limits.go) — catching an under-the-minimum
// value here means the create request never round-trips just to bounce off
// that same check server-side. Not a substitute for the server-side
// validation (a client can always skip this file entirely), just a faster
// failure for the normal case.
const MIN_CPU_CORES = 0.1; // 100m
const MIN_MEMORY_BYTES = 256 * 1024 ** 2; // 256Mi

// Create Environment dialog — mirrors create-agent-dialog.tsx's structure
// (gradient header, dialog footer actions, mutation-driven submit) but is a
// single step: a Name field by default, with an "Advanced" disclosure
// revealing an optional custom Image field. Per the design doc
// (docs/ai-agent/environment-management.md), most users should never touch
// the image field — it defaults server-side to the platform's pinned
// agent-server image when omitted.
export function EnvironmentCreateDialog({
	projectId,
	open,
	onOpenChange,
	onCreated,
}: {
	projectId: string;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onCreated?: (environment: Environment) => void;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const [name, setName] = useState("");
	const [showAdvanced, setShowAdvanced] = useState(false);
	const [image, setImage] = useState("");
	const [cpu, setCpu] = useState("");
	const [memory, setMemory] = useState("");
	const [disk, setDisk] = useState("");

	const reset = () => {
		setName("");
		setShowAdvanced(false);
		setImage("");
		setCpu("");
		setMemory("");
		setDisk("");
	};

	const handleClose = (v: boolean) => {
		if (!v) reset();
		onOpenChange(v);
	};

	const diskNumber = Number(disk);
	// Blank is always valid (falls through to the platform default, same as
	// every other field here) — only a non-blank, non-positive-integer
	// value is rejected.
	const diskValid =
		disk.trim() === "" || (Number.isInteger(diskNumber) && diskNumber > 0);
	const cpuValid =
		cpu.trim() === "" || parseCPULimitCores(cpu) >= MIN_CPU_CORES;
	const memoryValid =
		memory.trim() === "" || parseMemoryLimitBytes(memory) >= MIN_MEMORY_BYTES;

	const createMutation = useMutation({
		mutationFn: () =>
			createEnvironment(projectId, {
				name: name.trim(),
				...(image.trim() ? { image: image.trim() } : {}),
				...(cpu.trim() ? { cpu_limit: cpu.trim() } : {}),
				...(memory.trim() ? { memory_limit: memory.trim() } : {}),
				...(disk.trim() ? { disk_limit_gb: diskNumber } : {}),
			}),
		onSuccess: (environment) => {
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "environments"],
			});
			handleClose(false);
			onCreated?.(environment);
		},
	});

	const canSubmit =
		!!name.trim() &&
		diskValid &&
		cpuValid &&
		memoryValid &&
		!createMutation.isPending;

	return (
		<Dialog open={open} onOpenChange={handleClose}>
			<DialogContent className="sm:max-w-md p-0 gap-0 overflow-hidden">
				<div className="relative overflow-hidden border-b border-border/50">
					<div
						className="pointer-events-none absolute inset-0 opacity-[0.35]"
						style={{
							backgroundImage:
								"radial-gradient(circle, color-mix(in oklch, var(--color-primary) 14%, transparent) 1px, transparent 1px)",
							backgroundSize: "16px 16px",
						}}
					/>
					<div className="relative flex items-center gap-3 px-6 pt-5 pb-4">
						<div className="flex size-10 items-center justify-center rounded-xl bg-primary/10 ring-1 ring-primary/20 shadow-sm">
							<Server className="size-5 text-primary" />
						</div>
						<div>
							<DialogTitle className="text-sm font-semibold">
								{t("environments.createDialog.title")}
							</DialogTitle>
							<DialogDescription className="text-xs text-muted-foreground mt-0.5">
								{t("environments.createDialog.description")}
							</DialogDescription>
						</div>
					</div>
				</div>

				<div className="px-6 py-5 space-y-4">
					<div className="space-y-1.5">
						<Label htmlFor="environment-name">
							{t("environments.createDialog.nameLabel")}{" "}
							<span className="text-destructive">*</span>
						</Label>
						<Input
							id="environment-name"
							placeholder={t("environments.createDialog.namePlaceholder")}
							value={name}
							onChange={(e) => setName(e.target.value)}
							autoFocus
						/>
					</div>

					<div>
						<button
							type="button"
							onClick={() => setShowAdvanced((s) => !s)}
							className="flex items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
						>
							<ChevronRight
								className={cn(
									"size-3.5 transition-transform duration-150",
									showAdvanced && "rotate-90",
								)}
							/>
							{t("environments.createDialog.advanced")}
						</button>
						{showAdvanced && (
							<div className="space-y-4 mt-3">
								<div className="space-y-1.5">
									<Label htmlFor="environment-image">
										{t("environments.createDialog.imageLabel")}
									</Label>
									<Input
										id="environment-image"
										placeholder={t(
											"environments.createDialog.imagePlaceholder",
										)}
										value={image}
										onChange={(e) => setImage(e.target.value)}
										className="font-mono text-xs"
									/>
									<p className="text-xs text-muted-foreground">
										{t("environments.createDialog.imageHint")}
									</p>
								</div>

								<div>
									<div className="grid grid-cols-3 gap-3">
										<div className="space-y-1.5">
											<Label htmlFor="environment-cpu">
												{t("environments.detail.overview.vitals.cpu")}
											</Label>
											<Input
												id="environment-cpu"
												placeholder="2"
												value={cpu}
												onChange={(e) => setCpu(e.target.value)}
												className="font-mono text-xs"
												aria-invalid={!cpuValid}
											/>
										</div>
										<div className="space-y-1.5">
											<Label htmlFor="environment-memory">
												{t("environments.detail.overview.vitals.memory")}
											</Label>
											<Input
												id="environment-memory"
												placeholder="4Gi"
												value={memory}
												onChange={(e) => setMemory(e.target.value)}
												className="font-mono text-xs"
												aria-invalid={!memoryValid}
											/>
										</div>
										<div className="space-y-1.5">
											<Label htmlFor="environment-disk">
												{t("environments.detail.overview.vitals.disk")}
											</Label>
											<Input
												id="environment-disk"
												type="number"
												min={1}
												placeholder="20"
												value={disk}
												onChange={(e) => setDisk(e.target.value)}
												className="font-mono text-xs"
												aria-invalid={!diskValid}
											/>
										</div>
									</div>
									<p
										className={cn(
											"text-xs mt-1.5",
											cpuValid && memoryValid && diskValid
												? "text-muted-foreground"
												: "text-destructive",
										)}
									>
										{!cpuValid
											? t("environments.createDialog.cpuLimitInvalid")
											: !memoryValid
												? t("environments.createDialog.memoryLimitInvalid")
												: !diskValid
													? t("environments.createDialog.diskLimitInvalid")
													: t("environments.createDialog.resourcesHint")}
									</p>
								</div>
							</div>
						)}
					</div>

					{createMutation.isError && (
						<p className="text-sm text-destructive rounded-md bg-destructive/10 px-3 py-2">
							{t("environments.createDialog.createFailed")}
						</p>
					)}
				</div>

				<div className="border-t border-border/50 bg-muted/20 px-6 py-4 flex items-center justify-end gap-2">
					<Button
						variant="ghost"
						size="sm"
						onClick={() => handleClose(false)}
						className="text-muted-foreground"
					>
						{t("environments.createDialog.cancel")}
					</Button>
					<Button
						size="sm"
						onClick={() => createMutation.mutate()}
						disabled={!canSubmit}
					>
						{createMutation.isPending ? (
							<>
								<Loader2 className="size-4 mr-1.5 animate-spin" />
								{t("environments.createDialog.creating")}
							</>
						) : (
							<>
								<Sparkles className="size-4 mr-1.5" />
								{t("environments.createDialog.create")}
							</>
						)}
					</Button>
				</div>
			</DialogContent>
		</Dialog>
	);
}
