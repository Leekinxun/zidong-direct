import React from 'react';

interface Props {
  className?: string;
}

export const BrandLogo: React.FC<Props> = ({ className = 'h-8 w-8' }) => {
  return (
    <svg
      viewBox="0 0 64 64"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className={`shrink-0 select-none ${className}`}
    >
      <defs>
        <linearGradient id="zd-logo-bg" x1="6" y1="4" x2="58" y2="60" gradientUnits="userSpaceOnUse">
          <stop offset="0%" stopColor="#9333EA" />
          <stop offset="48%" stopColor="#5E5CE6" />
          <stop offset="100%" stopColor="#0A84FF" />
        </linearGradient>
        <linearGradient id="zd-logo-shine" x1="32" y1="2" x2="32" y2="34" gradientUnits="userSpaceOnUse">
          <stop offset="0%" stopColor="#FFFFFF" stopOpacity="0.34" />
          <stop offset="100%" stopColor="#FFFFFF" stopOpacity="0" />
        </linearGradient>
        <linearGradient id="zd-logo-bolt" x1="38" y1="34" x2="50" y2="51" gradientUnits="userSpaceOnUse">
          <stop offset="0%" stopColor="#FDE047" />
          <stop offset="100%" stopColor="#38BDF8" />
        </linearGradient>
      </defs>

      <rect x="3" y="3" width="58" height="58" rx="14" fill="url(#zd-logo-bg)" />
      <rect
        x="3.75"
        y="3.75"
        width="56.5"
        height="56.5"
        rx="13.25"
        stroke="url(#zd-logo-shine)"
        strokeWidth="1.5"
      />

      <ellipse cx="29" cy="19" rx="14.5" ry="5.5" fill="#FFFFFF" fillOpacity="0.95" />

      <path
        d="M14.5 25.5C14.5 28.54 20.99 31 29 31C37.01 31 43.5 28.54 43.5 25.5V32.5C43.5 35.54 37.01 38 29 38C20.99 38 14.5 35.54 14.5 32.5V25.5Z"
        fill="#FFFFFF"
        fillOpacity="0.82"
      />

      <path
        d="M14.5 36.5C14.5 39.54 20.99 42 29 42C37.01 42 43.5 39.54 43.5 36.5V43.5C43.5 46.54 37.01 49 29 49C20.99 49 14.5 46.54 14.5 43.5V36.5Z"
        fill="#FFFFFF"
        fillOpacity="0.68"
      />

      <circle cx="44.5" cy="42.5" r="11.5" fill="#161618" fillOpacity="0.88" />
      <circle cx="44.5" cy="42.5" r="10.5" stroke="#C4B5FD" strokeOpacity="0.6" strokeWidth="1.2" />
      <path
        d="M45.8 34.5L39.2 43.2H44.3L43.1 50.5L49.8 41.8H44.7L45.8 34.5Z"
        fill="url(#zd-logo-bolt)"
      />
    </svg>
  );
};
