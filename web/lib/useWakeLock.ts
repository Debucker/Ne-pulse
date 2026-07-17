"use client";

import { useCallback, useEffect, useRef, useState } from "react";

// The Wake Lock API isn't universal — Safari only shipped it in 16.4
// (March 2023), and some older Android WebViews still lack it entirely.
// `supported` lets a caller show/hide the toggle instead of pretending a
// lock is held when the browser silently can't grant one.
export interface WakeLockState {
  active: boolean;
  supported: boolean;
  enable: () => Promise<void>;
  disable: () => void;
}

/**
 * Requests a screen Wake Lock while enabled, keeping a phone's screen on
 * (and the tab from being suspended) for as long as the alarm needs to
 * watch the accelerometer — the whole point of a nightstand earthquake
 * monitor breaks the instant the OS dims the screen and throttles the page.
 *
 * The browser force-releases any wake lock the moment a tab is hidden
 * (switching apps, screen auto-lock overriding it, etc.), so this
 * re-acquires on `visibilitychange` for as long as the caller's own
 * `enable()` is still in effect — otherwise returning to the tab would
 * silently leave the screen unprotected again.
 */
export function useWakeLock(): WakeLockState {
  const [active, setActive] = useState(false);
  const [supported, setSupported] = useState(false);
  const sentinelRef = useRef<WakeLockSentinel | null>(null);
  const wantedRef = useRef(false);

  useEffect(() => {
    setSupported(typeof navigator !== "undefined" && "wakeLock" in navigator);
  }, []);

  const requestLock = useCallback(async () => {
    if (!("wakeLock" in navigator)) return;
    try {
      const sentinel = await navigator.wakeLock.request("screen");
      sentinelRef.current = sentinel;
      setActive(true);
      sentinel.addEventListener("release", () => {
        setActive(false);
      });
    } catch (err) {
      // Most commonly thrown when the document isn't visible yet — the
      // visibilitychange handler below will retry once it is.
      console.error("[WakeLock] request failed:", err);
      setActive(false);
    }
  }, []);

  const enable = useCallback(async () => {
    wantedRef.current = true;
    await requestLock();
  }, [requestLock]);

  const disable = useCallback(() => {
    wantedRef.current = false;
    sentinelRef.current?.release().catch(() => {});
    sentinelRef.current = null;
    setActive(false);
  }, []);

  useEffect(() => {
    function onVisibilityChange() {
      if (wantedRef.current && document.visibilityState === "visible" && !sentinelRef.current) {
        requestLock();
      }
    }
    document.addEventListener("visibilitychange", onVisibilityChange);
    return () => document.removeEventListener("visibilitychange", onVisibilityChange);
  }, [requestLock]);

  useEffect(() => {
    return () => {
      sentinelRef.current?.release().catch(() => {});
    };
  }, []);

  return { active, supported, enable, disable };
}
