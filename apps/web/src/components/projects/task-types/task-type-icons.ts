import type { LucideIcon } from "lucide-react";
import {
	AlertTriangle,
	BookOpen,
	Bug,
	CheckSquare,
	CircleDot,
	ClipboardList,
	FileText,
	Flame,
	FlaskConical,
	GitBranch,
	Hammer,
	Layers,
	Lightbulb,
	MessageSquare,
	Package,
	RefreshCw,
	Shield,
	Star,
	Target,
	Ticket,
	TrendingUp,
	Wrench,
	Zap,
} from "lucide-react";

export interface TaskTypeIconDef {
	name: string;
	component: LucideIcon;
	label: string;
}

export const TASK_TYPE_ICONS: TaskTypeIconDef[] = [
	{ name: "Bug", component: Bug, label: "Bug" },
	{ name: "Zap", component: Zap, label: "Feature" },
	{ name: "BookOpen", component: BookOpen, label: "Story" },
	{ name: "Layers", component: Layers, label: "Epic" },
	{ name: "CheckSquare", component: CheckSquare, label: "Task" },
	{ name: "Lightbulb", component: Lightbulb, label: "Idea" },
	{ name: "Shield", component: Shield, label: "Security" },
	{ name: "Wrench", component: Wrench, label: "Chore" },
	{ name: "GitBranch", component: GitBranch, label: "Branch" },
	{ name: "Flame", component: Flame, label: "Critical" },
	{ name: "Star", component: Star, label: "Important" },
	{ name: "Target", component: Target, label: "Goal" },
	{ name: "AlertTriangle", component: AlertTriangle, label: "Warning" },
	{ name: "FileText", component: FileText, label: "Doc" },
	{ name: "MessageSquare", component: MessageSquare, label: "Feedback" },
	{ name: "Package", component: Package, label: "Package" },
	{ name: "Hammer", component: Hammer, label: "Build" },
	{ name: "FlaskConical", component: FlaskConical, label: "Test" },
	{ name: "TrendingUp", component: TrendingUp, label: "Improvement" },
	{ name: "RefreshCw", component: RefreshCw, label: "Refactor" },
	{ name: "CircleDot", component: CircleDot, label: "Generic" },
	{ name: "Ticket", component: Ticket, label: "Ticket" },
	{ name: "ClipboardList", component: ClipboardList, label: "Checklist" },
];

export function getTaskTypeIconComponent(
	iconName: string | null | undefined,
): LucideIcon | null {
	if (!iconName) return null;
	return TASK_TYPE_ICONS.find((i) => i.name === iconName)?.component ?? null;
}
