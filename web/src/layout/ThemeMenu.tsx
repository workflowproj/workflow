import { Check, Monitor, Moon, Sun } from "lucide-react";
import { MenuItem, MenuLabel, Popover } from "@/components/ui";
import { useTheme, type ThemePreference } from "@/lib/theme";

const options: { value: ThemePreference; label: string; icon: typeof Sun }[] = [
  { value: "light", label: "Light", icon: Sun },
  { value: "dark", label: "Dark", icon: Moon },
  { value: "system", label: "System", icon: Monitor },
];

/** ThemeMenu switches between light, dark and the system setting (remembered per browser). */
export function ThemeMenu() {
  const { preference, theme, setPreference } = useTheme();
  const Current = preference === "system" ? Monitor : theme === "dark" ? Moon : Sun;
  return (
    <Popover
      align="end"
      trigger={({ toggle, open }) => (
        <button
          type="button"
          onClick={toggle}
          aria-label={`Theme: ${options.find((o) => o.value === preference)!.label}`}
          aria-haspopup="menu"
          aria-expanded={open}
          title="Theme"
          className="inline-flex size-8.5 items-center justify-center rounded-md text-muted hover:bg-surface-2 hover:text-ink"
        >
          <Current className="size-4.5" />
        </button>
      )}
      content={(close) => (
        <div role="menu" className="w-44">
          <MenuLabel>Theme</MenuLabel>
          {options.map((o) => (
            <MenuItem key={o.value} icon={<o.icon className="size-4" />} onClick={() => (setPreference(o.value), close())}>
              <span className="flex-1">{o.label}</span>
              {preference === o.value && <Check className="size-3.5 text-accent" />}
            </MenuItem>
          ))}
        </div>
      )}
    />
  );
}
