// Tiny i18n for the SDD Fleet plugin: Vietnamese (default), English, 中文.
// A flat key→string map per language; t(key) falls back vi/zh → en → key.
import { LS_LANG } from "./config";

export type Lang = "vi" | "en" | "zh";

export const LANGS: { code: Lang; label: string }[] = [
	{ code: "vi", label: "VI" },
	{ code: "en", label: "EN" },
	{ code: "zh", label: "中文" },
];

type Dict = Record<string, string>;

const en: Dict = {
	"app.title": "SDD Fleet",
	"app.sub": "Spec-Driven Development sensor fleet — team-wide",
	"act.refresh": "Refresh",
	"state.loading": "Loading…",
	"state.error": "Could not load SDD data",
	"state.sessionExpired": "Session expired",
	"state.sessionExpiredBody":
		"Your Paca session is no longer valid. Reload the app to sign back in, then reopen this page.",
	"state.retry": "Retry",
	"state.empty": "Nothing here yet",
	"state.asOf": "as of",

	"nav.overview": "Overview",
	"nav.tasks": "Task board",
	"nav.sessions": "Sessions",
	"nav.activity": "Activity",
	"nav.analytics": "Analytics",
	"nav.coordination": "Coordination",
	"nav.sdd": "SDD phases",
	"nav.fleet": "Fleet",

	"overview.title": "Team overview",
	"overview.sub": "Whole-fleet coordination — machines, people, tasks, conflicts, gates",
	"overview.machinesOnline": "Machines online",
	"overview.devActive": "Devs active",
	"overview.sessionsActive": "Sessions active",
	"overview.totalEvents": "Total events",
	"overview.openConflicts": "Open conflicts",
	"overview.gatesPending": "Gates pending",
	"overview.tasksByStatus": "Tasks by status",
	"overview.recent": "Recent team activity",

	"tasks.title": "Task coordination",
	"tasks.sub": "Cross-machine task board with live per-assignee SDD phase (read-only)",
	"tasks.readonly": "Read-only — task editing lives in Paca (ADR-038)",
	"tasks.unassigned": "unassigned",
	"tasks.live": "live",

	"sessions.title": "Sessions",
	"sessions.sub": "Recent Claude Code sessions reporting into the fleet",
	"sessions.repo": "Repo",
	"sessions.host": "Host",
	"sessions.user": "User",
	"sessions.status": "Status",
	"sessions.updated": "Updated",

	"activity.title": "Activity",
	"activity.sub": "Live hook-event stream across the fleet",
	"activity.event": "Event",
	"activity.tool": "Tool",
	"activity.who": "Who",
	"activity.when": "When",

	"analytics.title": "Team analytics",
	"analytics.sub": "Whole-team activity by person, machine, repo, phase and level",
	"analytics.byDev": "By developer",
	"analytics.byMachine": "By machine",
	"analytics.byRepo": "By repo",
	"analytics.phaseDist": "Phase distribution",
	"analytics.levelDist": "Governance-level distribution",
	"analytics.daily": "Activity — last 14 days",

	"coord.title": "Coordination",
	"coord.sub": "Cross-machine coordination — Shared Core conflicts and who works where",
	"coord.openConflicts": "Open conflicts",
	"coord.noConflicts": "No conflicts — nobody is stepping on anybody",
	"coord.parallelByRepo": "Parallel work by repo",

	"sdd.title": "SDD phases",
	"sdd.sub": "Spec-Driven SDLC — 8-phase agent board, governance mix, spec versions, flags",
	"sdd.phaseBoard": "Phase board",
	"sdd.governance": "Governance level mix",
	"sdd.specVersions": "Spec versions",
	"sdd.sharedCore": "Shared Core touches",
	"sdd.unapprovedL3": "Unapproved L3+",
	"sdd.flags": "Governance flags",
	"sdd.noFlags": "No open flags — governance is clean",
	"sdd.noSpecs": "No spec versions tracked yet",
	"sdd.noAgents": "no agents",
	"sdd.implemented": "implemented",
	"sdd.recent": "Recent SDD activity",
	"sdd.l1": "L1 Read",
	"sdd.l2": "L2 Branch",
	"sdd.l3": "L3 Shared Core",
	"sdd.l4": "L4 Merge/Deploy",

	"fleet.title": "Machine fleet",
	"fleet.sub": "Machines/devs reporting in — status, sessions, current phase",
	"fleet.online": "online",
	"fleet.sessions": "sessions",
};

const vi: Dict = {
	"app.title": "SDD Fleet",
	"app.sub": "Đội cảm biến Spec-Driven Development — toàn đội",
	"act.refresh": "Làm mới",
	"state.loading": "Đang tải…",
	"state.error": "Không tải được dữ liệu SDD",
	"state.sessionExpired": "Phiên đã hết hạn",
	"state.sessionExpiredBody":
		"Phiên Paca của bạn không còn hiệu lực. Tải lại ứng dụng để đăng nhập lại rồi mở lại trang này.",
	"state.retry": "Thử lại",
	"state.empty": "Chưa có gì ở đây",
	"state.asOf": "lúc",

	"nav.overview": "Tổng quan",
	"nav.tasks": "Bảng task",
	"nav.sessions": "Phiên",
	"nav.activity": "Luồng hoạt động",
	"nav.analytics": "Phân tích",
	"nav.coordination": "Điều phối",
	"nav.sdd": "Giai đoạn SDD",
	"nav.fleet": "Fleet máy",

	"overview.title": "Tổng quan đội",
	"overview.sub": "Toàn cảnh điều phối cả đội — máy, người, task, xung đột, cổng duyệt",
	"overview.machinesOnline": "Máy online",
	"overview.devActive": "Dev active",
	"overview.sessionsActive": "Phiên active",
	"overview.totalEvents": "Tổng event",
	"overview.openConflicts": "Xung đột mở",
	"overview.gatesPending": "Gate chờ",
	"overview.tasksByStatus": "Task theo trạng thái",
	"overview.recent": "Hoạt động đội gần đây",

	"tasks.title": "Điều phối task",
	"tasks.sub": "Bảng task chéo máy, hiện giai đoạn SDD live của người nhận (chỉ đọc)",
	"tasks.readonly": "Chỉ đọc — chỉnh sửa task nằm ở Paca (ADR-038)",
	"tasks.unassigned": "chưa giao",
	"tasks.live": "live",

	"sessions.title": "Phiên",
	"sessions.sub": "Các phiên Claude Code gần đây báo về fleet",
	"sessions.repo": "Repo",
	"sessions.host": "Máy",
	"sessions.user": "Người",
	"sessions.status": "Trạng thái",
	"sessions.updated": "Cập nhật",

	"activity.title": "Luồng hoạt động",
	"activity.sub": "Dòng sự kiện hook live của cả fleet",
	"activity.event": "Sự kiện",
	"activity.tool": "Công cụ",
	"activity.who": "Ai",
	"activity.when": "Khi nào",

	"analytics.title": "Phân tích đội",
	"analytics.sub": "Hoạt động cả đội theo người, máy, repo, giai đoạn, mức phân quyền",
	"analytics.byDev": "Theo dev",
	"analytics.byMachine": "Theo máy",
	"analytics.byRepo": "Theo repo",
	"analytics.phaseDist": "Phân bố giai đoạn",
	"analytics.levelDist": "Phân bố mức phân quyền",
	"analytics.daily": "Hoạt động 14 ngày",

	"coord.title": "Điều phối",
	"coord.sub": "Điều phối chéo máy — xung đột Shared Core và ai làm ở repo nào",
	"coord.openConflicts": "Xung đột mở",
	"coord.noConflicts": "Không có xung đột — chưa ai giẫm chân nhau",
	"coord.parallelByRepo": "Làm song song theo repo",

	"sdd.title": "Giai đoạn SDD",
	"sdd.sub": "SDLC theo spec — bảng 8 giai đoạn, mức phân quyền, phiên bản spec, cờ",
	"sdd.phaseBoard": "Bảng giai đoạn",
	"sdd.governance": "Mức phân quyền",
	"sdd.specVersions": "Phiên bản spec",
	"sdd.sharedCore": "Chạm Shared Core",
	"sdd.unapprovedL3": "L3+ chưa duyệt",
	"sdd.flags": "Cờ quản trị",
	"sdd.noFlags": "Không có cờ — quản trị sạch",
	"sdd.noSpecs": "Chưa theo dõi phiên bản spec nào",
	"sdd.noAgents": "chưa có agent",
	"sdd.implemented": "đã triển khai",
	"sdd.recent": "Hoạt động SDD gần đây",
	"sdd.l1": "L1 Đọc",
	"sdd.l2": "L2 Nhánh",
	"sdd.l3": "L3 Shared Core",
	"sdd.l4": "L4 Merge/Deploy",

	"fleet.title": "Fleet máy",
	"fleet.sub": "Máy/dev đang báo về — trạng thái, phiên, giai đoạn hiện tại",
	"fleet.online": "online",
	"fleet.sessions": "phiên",
};

const zh: Dict = {
	"app.title": "SDD 机群",
	"app.sub": "规范驱动开发传感器机群 — 全团队",
	"act.refresh": "刷新",
	"state.loading": "加载中…",
	"state.error": "无法加载 SDD 数据",
	"state.sessionExpired": "会话已过期",
	"state.sessionExpiredBody": "Paca 会话已失效。请重新加载应用登录后再打开此页面。",
	"state.retry": "重试",
	"state.empty": "暂无内容",
	"state.asOf": "截至",

	"nav.overview": "总览",
	"nav.tasks": "任务板",
	"nav.sessions": "会话",
	"nav.activity": "活动",
	"nav.analytics": "分析",
	"nav.coordination": "协调",
	"nav.sdd": "SDD 阶段",
	"nav.fleet": "机群",

	"overview.title": "团队总览",
	"overview.sub": "全机群协调 — 机器、人员、任务、冲突、审批门",
	"overview.machinesOnline": "在线机器",
	"overview.devActive": "活跃开发者",
	"overview.sessionsActive": "活跃会话",
	"overview.totalEvents": "事件总数",
	"overview.openConflicts": "未决冲突",
	"overview.gatesPending": "待审门",
	"overview.tasksByStatus": "按状态的任务",
	"overview.recent": "近期团队活动",

	"tasks.title": "任务协调",
	"tasks.sub": "跨机任务板，显示受让人实时 SDD 阶段（只读）",
	"tasks.readonly": "只读 — 任务编辑在 Paca 中（ADR-038）",
	"tasks.unassigned": "未分配",
	"tasks.live": "实时",

	"sessions.title": "会话",
	"sessions.sub": "近期报告到机群的 Claude Code 会话",
	"sessions.repo": "仓库",
	"sessions.host": "主机",
	"sessions.user": "用户",
	"sessions.status": "状态",
	"sessions.updated": "更新",

	"activity.title": "活动",
	"activity.sub": "全机群实时 hook 事件流",
	"activity.event": "事件",
	"activity.tool": "工具",
	"activity.who": "谁",
	"activity.when": "时间",

	"analytics.title": "团队分析",
	"analytics.sub": "按人员、机器、仓库、阶段与级别的全队活动",
	"analytics.byDev": "按开发者",
	"analytics.byMachine": "按机器",
	"analytics.byRepo": "按仓库",
	"analytics.phaseDist": "阶段分布",
	"analytics.levelDist": "治理级别分布",
	"analytics.daily": "近 14 天活动",

	"coord.title": "协调",
	"coord.sub": "跨机协调 — Shared Core 冲突与分工",
	"coord.openConflicts": "未决冲突",
	"coord.noConflicts": "无冲突 — 没有人互相干扰",
	"coord.parallelByRepo": "按仓库并行工作",

	"sdd.title": "SDD 阶段",
	"sdd.sub": "规范驱动 SDLC — 8 阶段看板、治理级别、规范版本、标记",
	"sdd.phaseBoard": "阶段看板",
	"sdd.governance": "治理级别",
	"sdd.specVersions": "规范版本",
	"sdd.sharedCore": "Shared Core 触碰",
	"sdd.unapprovedL3": "未批 L3+",
	"sdd.flags": "治理标记",
	"sdd.noFlags": "无未决标记 — 治理干净",
	"sdd.noSpecs": "尚未跟踪任何规范版本",
	"sdd.noAgents": "暂无代理",
	"sdd.implemented": "已实现",
	"sdd.recent": "近期 SDD 活动",
	"sdd.l1": "L1 读取",
	"sdd.l2": "L2 分支",
	"sdd.l3": "L3 Shared Core",
	"sdd.l4": "L4 合并/部署",

	"fleet.title": "机器机群",
	"fleet.sub": "报告中的机器/开发者 — 状态、会话、当前阶段",
	"fleet.online": "在线",
	"fleet.sessions": "会话",
};

const DICTS: Record<Lang, Dict> = { vi, en, zh };

export type T = (key: string) => string;

export function makeT(lang: Lang): T {
	const d = DICTS[lang] || en;
	return (key: string) => d[key] ?? en[key] ?? key;
}

export function detectLang(): Lang {
	if (typeof localStorage !== "undefined") {
		const saved = localStorage.getItem(LS_LANG);
		if (saved === "vi" || saved === "en" || saved === "zh") return saved;
	}
	if (typeof navigator !== "undefined") {
		const n = (navigator.language || "").toLowerCase();
		if (n.startsWith("vi")) return "vi";
		if (n.startsWith("zh")) return "zh";
		if (n.startsWith("en")) return "en";
	}
	return "vi";
}

export function saveLang(lang: Lang): void {
	if (typeof localStorage !== "undefined") localStorage.setItem(LS_LANG, lang);
}
