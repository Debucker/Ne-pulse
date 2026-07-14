"use client";

import { motion } from "framer-motion";
import type { ReactNode } from "react";

// A reusable scroll-triggered fade-and-rise, the base building block for
// the Apple-style "content settles in as you scroll" feel across the
// landing page. Fires once, the moment the element crosses into view.
export default function Reveal({
  children,
  className = "",
  delay = 0,
  y = 24,
}: {
  children?: ReactNode;
  className?: string;
  delay?: number;
  y?: number;
}) {
  return (
    <motion.div
      className={className}
      initial={{ opacity: 0, y }}
      whileInView={{ opacity: 1, y: 0 }}
      viewport={{ once: true, amount: 0.25 }}
      transition={{ duration: 0.7, ease: [0.16, 1, 0.3, 1], delay }}
    >
      {children}
    </motion.div>
  );
}
