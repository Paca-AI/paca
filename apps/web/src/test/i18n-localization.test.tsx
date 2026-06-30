import { act, render, screen } from "@testing-library/react";
import { afterAll, beforeEach, describe, expect, it, vi } from "vitest";

import i18n from "@/i18n";

// Mirror the existing test convention: stub react-query so components that
// call useQuery/useMutation render without a real provider. `rq` is mutable so
// individual tests can drive loading/empty states.
const rq = vi.hoisted(() => ({ isLoading: false, data: [] as unknown[] }));

vi.mock("@tanstack/react-query", async (importOriginal) => {
	const actual =
		await importOriginal<typeof import("@tanstack/react-query")>();
	return {
		...actual,
		useQueryClient: () => ({ invalidateQueries: vi.fn() }),
		useMutation: () => ({
			mutate: vi.fn(),
			isPending: false,
			variables: undefined,
		}),
		useQuery: () => ({ data: rq.data, isLoading: rq.isLoading }),
	};
});

import { GlobalRolesHeader } from "@/components/admin/global-roles/GlobalRolesHeader";
import { GlobalRolesStats } from "@/components/admin/global-roles/GlobalRolesStats";
import { RoleFormDialog } from "@/components/admin/global-roles/RoleFormDialog";
import { PluginMarketplacePanel } from "@/components/plugins/PluginMarketplacePanel";
import { ChangePasswordCard } from "@/components/profile/ChangePasswordCard";
import {
	validateNewPassword,
	validateUsername,
} from "@/lib/auth-validation";

async function setLang(lng: string) {
	await act(async () => {
		await i18n.changeLanguage(lng);
	});
}

afterAll(async () => {
	await i18n.changeLanguage("en");
});

describe("i18n localization", () => {
	beforeEach(() => {
		rq.isLoading = false;
		rq.data = [];
	});

	describe("global roles", () => {
		it("renders header title + action in en and ko", async () => {
			await setLang("en");
			const en = render(<GlobalRolesHeader canWrite onCreate={() => {}} />);
			expect(en.container.textContent).toContain("Global Roles");
			expect(en.container.textContent).toContain("New Role");
			en.unmount();

			await setLang("ko");
			const ko = render(<GlobalRolesHeader canWrite onCreate={() => {}} />);
			expect(ko.container.textContent).toContain("전역 역할");
			expect(ko.container.textContent).toContain("새 역할");
		});

		it("renders pluralized stats in en and ko", async () => {
			await setLang("en");
			const one = render(<GlobalRolesStats rolesCount={1} totalGranted={5} />);
			expect(one.container.textContent).toContain("role defined");
			expect(one.container.textContent).not.toContain("roles defined");
			one.unmount();

			const many = render(<GlobalRolesStats rolesCount={3} totalGranted={5} />);
			expect(many.container.textContent).toContain("3");
			expect(many.container.textContent).toContain("roles defined");
			expect(many.container.textContent).toContain(
				"permission grants across all roles",
			);
			many.unmount();

			await setLang("ko");
			const ko = render(<GlobalRolesStats rolesCount={3} totalGranted={5} />);
			expect(ko.container.textContent).toContain("정의된 역할");
			expect(ko.container.textContent).toContain("전체 역할의 권한 부여");
		});

		it("resolves dynamic permission-catalog keys in the role form (ko)", async () => {
			await setLang("ko");
			render(<RoleFormDialog open onOpenChange={() => {}} />);

			// Permission group label: admin.globalRoles.permGroups.global_roles
			expect(screen.getAllByText("전역 역할").length).toBeGreaterThan(0);
			// Per-permission dynamic key path:
			// admin.globalRoles.perms.global_roles_read.{label,desc}
			expect(screen.getByText("전역 역할 읽기")).toBeInTheDocument();
			expect(screen.getByText("전역 역할 정의 보기")).toBeInTheDocument();
			// Raw permission key must NOT leak (would mean key lookup failed)
			expect(screen.queryByText("global_roles.read")).not.toBeInTheDocument();
		});
	});

	describe("plugins", () => {
		it("renders marketplace loading + empty states (ko)", async () => {
			await setLang("ko");

			rq.isLoading = true;
			const loading = render(<PluginMarketplacePanel />);
			expect(loading.container.textContent).toContain(
				"마켓플레이스를 불러오는 중…",
			);
			loading.unmount();

			rq.isLoading = false;
			rq.data = [];
			render(<PluginMarketplacePanel />);
			expect(screen.getByText("마켓플레이스 플러그인이 없습니다.")).toBeInTheDocument();
			expect(screen.getByPlaceholderText("플러그인 검색")).toBeInTheDocument();
		});

		it("has plugin + extension-point keys defined in both languages", () => {
			expect(i18n.getFixedT("en")("admin.plugins.title")).toBe(
				"Plugin Settings",
			);
			expect(i18n.getFixedT("ko")("admin.plugins.title")).toBe("플러그인 설정");
			// Extension-point dynamic key path (sidebar.general.section -> _ key)
			expect(
				i18n.getFixedT("ko")("admin.plugins.extPoints.sidebar_general_section"),
			).toBe("사이드바 — 일반");
			expect(i18n.getFixedT("en")("admin.plugins.extPoints.view")).toBe(
				"Custom View",
			);
		});
	});

	describe("profile", () => {
		it("renders the change-password card in ko", async () => {
			await setLang("ko");
			const { container } = render(<ChangePasswordCard mustChange={false} />);
			// title + submit button both read "비밀번호 변경"
			expect(container.textContent).toContain("비밀번호 변경");
			expect(container.textContent).toContain("현재 비밀번호");
			expect(container.textContent).toContain("새 비밀번호");
			expect(container.textContent).toContain("새 비밀번호 확인");
		});

		it("localizes shared auth validation messages (en + ko)", () => {
			const en = i18n.getFixedT("en");
			const ko = i18n.getFixedT("ko");
			// English values must stay byte-identical (existing unit tests rely on it)
			expect(validateUsername("", en)).toBe("Username is required.");
			expect(validateNewPassword("short", en)).toBe(
				"New password must be at least 8 characters.",
			);
			// Korean localization
			expect(validateUsername("", ko)).toBe("사용자 이름을 입력하세요.");
			expect(validateNewPassword("short", ko)).toBe(
				"새 비밀번호는 최소 8자 이상이어야 합니다.",
			);
		});
	});
});
