import { useEffect, useRef, useState, type ComponentProps, type PointerEvent } from 'react';
import { TerminalWorkspace } from './TerminalWorkspace';
import type { TerminalEncoding, TerminalTarget } from './TerminalSession';

type Direction = 'columns' | 'rows';
type Leaf = { kind: 'terminal'; id: number; target?: TerminalTarget; encoding?: TerminalEncoding };
type Branch = { kind: 'split'; id: number; direction: Direction; ratio: number; first: Layout; second: Layout };
type Layout = Leaf | Branch;
type Bounds = { x: number; y: number; width: number; height: number };
type LeafPosition = { pane: Leaf; bounds: Bounds };
type Divider = { split: Branch; bounds: Bounds };

const MIN_RATIO = 0.15;
const MAX_RATIO = 0.85;

function updateSplit(layout: Layout, id: number, ratio: number): Layout {
  if (layout.kind === 'terminal') return layout;
  if (layout.id === id) return { ...layout, ratio: Math.min(MAX_RATIO, Math.max(MIN_RATIO, ratio)) };
  return { ...layout, first: updateSplit(layout.first, id, ratio), second: updateSplit(layout.second, id, ratio) };
}

function splitPane(layout: Layout, id: number, branchID: number, next: Leaf, direction: Direction): Layout {
  if (layout.kind === 'terminal') {
    return layout.id === id ? { kind: 'split', id: branchID, direction, ratio: 0.5, first: layout, second: next } : layout;
  }
  return { ...layout, first: splitPane(layout.first, id, branchID, next, direction), second: splitPane(layout.second, id, branchID, next, direction) };
}

function removePane(layout: Layout, id: number): Layout | null {
  if (layout.kind === 'terminal') return layout.id === id ? null : layout;
  const first = removePane(layout.first, id);
  const second = removePane(layout.second, id);
  return first && second ? { ...layout, first, second } : first || second;
}

function position(layout: Layout, bounds: Bounds, panes: LeafPosition[], dividers: Divider[]) {
  if (layout.kind === 'terminal') {
    panes.push({ pane: layout, bounds });
    return;
  }
  dividers.push({ split: layout, bounds });
  const { x, y, width, height } = bounds;
  const ratio = layout.ratio;
  const sideBySide = layout.direction === 'columns';
  position(layout.first, { x, y, width: sideBySide ? width * ratio : width, height: sideBySide ? height : height * ratio }, panes, dividers);
  position(layout.second, {
    x: sideBySide ? x + width * ratio : x, y: sideBySide ? y : y + height * ratio,
    width: sideBySide ? width * (1 - ratio) : width, height: sideBySide ? height : height * (1 - ratio),
  }, panes, dividers);
}

type Props = Omit<ComponentProps<typeof TerminalWorkspace>, 'onSplit' | 'onClosePane'> & { initialTitle: string };

export function TerminalPanes({ initialTitle, onDestinationChange, ...props }: Props) {
  const [layout, setLayout] = useState<Layout>({ kind: 'terminal', id: 0 });
  const [selected, setSelected] = useState(0);
  const [titles, setTitles] = useState<Record<number, string>>(() => ({ 0: initialTitle }));
  const nextID = useRef(0);
  const container = useRef<HTMLDivElement>(null);
  const reportTitle = useRef(onDestinationChange);
  reportTitle.current = onDestinationChange;
  const panes: LeafPosition[] = [];
  const dividers: Divider[] = [];
  position(layout, { x: 0, y: 0, width: 100, height: 100 }, panes, dividers);
  const activePane = panes.some(({ pane }) => pane.id === selected) ? selected : panes[0].pane.id;
  const activeTitle = titles[activePane] ?? 'SSH 终端';

  useEffect(() => {
    reportTitle.current?.(activeTitle);
  }, [activeTitle]);

  function drag(event: PointerEvent<HTMLDivElement>, divider: Divider) {
    if (!event.currentTarget.hasPointerCapture(event.pointerId) || !container.current) return;
    const rect = container.current.getBoundingClientRect();
    const { split, bounds } = divider;
    const axis = split.direction === 'columns'
      ? { mouse: event.clientX - rect.left, length: rect.width, start: bounds.x, size: bounds.width }
      : { mouse: event.clientY - rect.top, length: rect.height, start: bounds.y, size: bounds.height };
    if (axis.length > 0) {
      const ratio = (axis.mouse / axis.length * 100 - axis.start) / axis.size;
      setLayout((previous) => updateSplit(previous, split.id, ratio));
    }
  }

  return (
    <div ref={container} className="relative h-full min-h-0 overflow-hidden">
      {panes.map(({ pane, bounds }) => {
        const target = pane.target;
        const selectedPane = pane.id === activePane;
        return (
          <div
            key={pane.id}
            className={`absolute min-w-0 overflow-hidden border ${panes.length === 1 ? 'border-transparent' : selectedPane ? 'border-indigo-500/60' : 'border-slate-700'}`}
            style={{ left: `${bounds.x}%`, top: `${bounds.y}%`, width: `${bounds.width}%`, height: `${bounds.height}%` }}
            onPointerDown={() => setSelected(pane.id)}
            onFocus={() => setSelected(pane.id)}
          >
            <TerminalWorkspace
              {...props}
              active={props.active && selectedPane}
              {...(pane.id === 0 ? {} : {
                autoConnect: Boolean(target),
                initialSource: target && !('ssh' in target) ? 'saved' as const : 'direct' as const,
                initialConnectionId: target && 'connectionId' in target ? target.connectionId : undefined,
                initialHostId: target && 'sshHostId' in target ? target.sshHostId : undefined,
                initialSettings: target && 'ssh' in target ? target.ssh : undefined,
                initialEncoding: pane.encoding,
              })}
              onDestinationChange={(title) => setTitles((previous) => (
                previous[pane.id] === title ? previous : { ...previous, [pane.id]: title }
              ))}
              onSplit={(direction, target, encoding) => {
                const next: Leaf = { kind: 'terminal', id: ++nextID.current, target, encoding };
                const branchID = ++nextID.current;
                setLayout((previous) => splitPane(previous, pane.id, branchID, next, direction));
                setSelected(next.id);
              }}
              onClosePane={panes.length > 1 ? () => {
                setLayout((previous) => removePane(previous, pane.id) || previous);
                setTitles((previous) => {
                  const next = { ...previous };
                  delete next[pane.id];
                  return next;
                });
              } : undefined}
            />
          </div>
        );
      })}
      {dividers.map(({ split, bounds }) => {
        const columns = split.direction === 'columns';
        const style = columns
          ? { left: `${bounds.x + bounds.width * split.ratio}%`, top: `${bounds.y}%`, width: 8, height: `${bounds.height}%`, transform: 'translateX(-50%)' }
          : { left: `${bounds.x}%`, top: `${bounds.y + bounds.height * split.ratio}%`, width: `${bounds.width}%`, height: 8, transform: 'translateY(-50%)' };
        return (
          <div
            key={split.id}
            role="separator"
            aria-label="调整分屏大小"
            aria-orientation={columns ? 'vertical' : 'horizontal'}
            aria-valuemin={15}
            aria-valuemax={85}
            aria-valuenow={Math.round(split.ratio * 100)}
            tabIndex={0}
            style={style}
            className={`absolute z-20 touch-none hover:bg-indigo-400/50 focus:bg-indigo-400/50 ${columns ? 'cursor-col-resize' : 'cursor-row-resize'}`}
            onPointerDown={(event) => {
              if (event.button !== 0) return;
              event.currentTarget.setPointerCapture(event.pointerId);
              event.preventDefault();
            }}
            onPointerMove={(event) => drag(event, { split, bounds })}
            onKeyDown={(event) => {
              const direction = event.key === (columns ? 'ArrowLeft' : 'ArrowUp') ? -1
                : event.key === (columns ? 'ArrowRight' : 'ArrowDown') ? 1 : 0;
              if (!direction) return;
              event.preventDefault();
              setLayout((previous) => updateSplit(previous, split.id, split.ratio + direction * 0.05));
            }}
          />
        );
      })}
    </div>
  );
}
