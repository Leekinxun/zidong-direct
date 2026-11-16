import { useEffect, useRef, useSyncExternalStore, type RefObject } from 'react';
import {
  adjustDatabaseFontSize, adjustTerminalFontSize, readDatabaseFontSize, readTerminalFontSize, subscribeToFontSize,
} from './preferences';

export function useWheelFontZoom(target: RefObject<HTMLElement>, onStep: (step: number) => void) {
  const handler = useRef(onStep);
  handler.current = onStep;

  useEffect(() => {
    const element = target.current;
    if (!element) return;
    let distance = 0;
    let lastEvent = 0;
    const onWheel = (event: WheelEvent) => {
      if ((!event.ctrlKey && !event.metaKey) || event.deltaY === 0) {
        distance = 0;
        return;
      }
      event.preventDefault();
      event.stopPropagation();

      if (event.timeStamp - lastEvent > 200 || Math.sign(distance) !== Math.sign(event.deltaY)) {
        distance = 0;
      }
      lastEvent = event.timeStamp;
      distance += event.deltaMode === WheelEvent.DOM_DELTA_PIXEL ? event.deltaY : Math.sign(event.deltaY) * 40;
      if (Math.abs(distance) >= 40) {
        handler.current(-Math.sign(distance));
        distance = 0;
      }
    };
    element.addEventListener('wheel', onWheel, { capture: true, passive: false });
    return () => element.removeEventListener('wheel', onWheel, true);
  }, [target]);
}

export function useDatabaseFontSize(): [number, (step: number) => void] {
  return [useSyncExternalStore(subscribeToFontSize, readDatabaseFontSize), adjustDatabaseFontSize];
}

export function useTerminalFontSize(): [number, (step: number) => void] {
  return [useSyncExternalStore(subscribeToFontSize, readTerminalFontSize), adjustTerminalFontSize];
}
