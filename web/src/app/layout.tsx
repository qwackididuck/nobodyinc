import type { Metadata } from "next";
import { Inter, Pixelify_Sans, Silkscreen, VT323 } from "next/font/google";

import "./globals.css";

// Fonts are downloaded at build time and served with the app: the browser
// never calls Google. Each one sets a CSS variable used by globals.css.
const inter = Inter({ subsets: ["latin"], variable: "--font-inter", display: "swap" });
const pixelify = Pixelify_Sans({ subsets: ["latin"], variable: "--font-pixelify", display: "swap" });
const silkscreen = Silkscreen({ subsets: ["latin"], weight: "400", variable: "--font-silkscreen", display: "swap" });
const vt323 = VT323({ subsets: ["latin"], weight: "400", variable: "--font-vt323", display: "swap" });

export const metadata: Metadata = {
  title: "Nobody Inc",
  description: "Control room of the Nobody Inc agent organisation",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className={`${inter.variable} ${pixelify.variable} ${silkscreen.variable} ${vt323.variable}`}>
      <body>{children}</body>
    </html>
  );
}
