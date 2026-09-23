import { Button } from "@base-ui/react/button";
import { Fragment, useEffect, useRef } from "react";
import { useDashboard } from "../../data/DashboardContext";
import { useSiteStats } from "../../data/useSiteStats";
import { UNKNOWN } from "../../domain/format";
import type { SiteDTO } from "../../domain/types";
import { liveCallouts, type Callout } from "../../guide/live";
import { LESSONS, LESSON_BY_ID, type LessonId } from "../../guide/lessons";
import { useNow } from "../../hooks/useNow";
import { LevelMark, toneClass } from "../Health/LevelMark";
import shared from "../Dashboard/Dashboard.module.css";
import styles from "./GuideView.module.css";

interface GuideViewProps {
  lesson: LessonId;
  onLessonChange: (lesson: LessonId) => void;
  sites: SiteDTO[];
}

/** Renders `code` spans written with backticks in lesson text; everything else stays plain text. */
function Rich({ text }: { text: string }) {
  const parts = text.split("`");
  return (
    <>
      {parts.map((part, i) =>
        i % 2 === 1 ? (
          <code key={i} className={styles.code}>
            {part}
          </code>
        ) : (
          <Fragment key={i}>{part}</Fragment>
        ),
      )}
    </>
  );
}

function CalloutItem({ c }: { c: Callout }) {
  return (
    <li className={styles.callout}>
      <span className={styles.subject}>{c.subject}</span>
      <span className={toneClass} data-level={c.level}>
        {c.text}
      </span>
      {c.level && <LevelMark assessment={{ level: c.level, reason: c.text }} />}
    </li>
  );
}

/** Today's numbers for one site: the analytics lesson's live values (sites have no live stream). */
function SiteToday({ site }: { site: SiteDTO }) {
  const today = useSiteStats(site.id, "today");
  const n = (v: number | undefined) =>
    today.status === "ready" ? String(v ?? 0) : UNKNOWN;
  return (
    <CalloutItem
      c={{
        subject: site.name,
        text: `today (UTC): ${n(today.stats?.pageviews)} pageviews, ${n(today.stats?.visitors)} visitors`,
      }}
    />
  );
}

/**
 * The guide (docs/SPEC.md §5, Change 22): a step-by-step course on reading the dashboard, one lesson
 * at a time, each next to the live values of the operator's own hosts.
 */
export function GuideView({ lesson, onLessonChange, sites }: GuideViewProps) {
  const state = useDashboard();
  const now = useNow(5000);
  const heading = useRef<HTMLHeadingElement>(null);
  const index = LESSONS.findIndex((l) => l.id === lesson);
  const l = LESSON_BY_ID[lesson];
  const prev = LESSONS[index - 1];
  const next = LESSONS[index + 1];
  const live = liveCallouts(lesson, state, now);
  const firstRender = useRef(true);

  useEffect(() => {
    // Moving between lessons (not the first paint) puts focus and view on the new lesson.
    if (firstRender.current) {
      firstRender.current = false;
      return;
    }
    heading.current?.focus();
    heading.current?.scrollIntoView?.({ block: "start" });
  }, [lesson]);

  return (
    <div className={styles.guide}>
      <nav className={styles.toc} aria-label="Guide lessons">
        <h2 className={shared.sectionTitle}>Guide</h2>
        <ol className={styles.tocList}>
          {LESSONS.map((item, i) => (
            <li key={item.id}>
              <button
                type="button"
                className={styles.tocItem}
                aria-current={item.id === lesson ? "step" : undefined}
                onClick={() => onLessonChange(item.id)}
              >
                <span className={styles.tocNumber}>
                  {String(i).padStart(2, "0")}
                </span>{" "}
                {item.title}
              </button>
            </li>
          ))}
        </ol>
      </nav>

      <article
        className={styles.lesson}
        aria-labelledby="lesson-title"
        id={`lesson-${l.id}`}
      >
        <p className={styles.step}>
          lesson {index} of {LESSONS.length - 1}
        </p>
        <h2
          id="lesson-title"
          ref={heading}
          tabIndex={-1}
          className={styles.title}
        >
          {l.title}
        </h2>
        <p className={styles.lead}>{l.summary}</p>

        {(live.length > 0 || (lesson === "analytics" && sites.length > 0)) && (
          <section
            className={styles.live}
            aria-label="Right now on your servers"
          >
            <h3 className={styles.blockTitle}>Right now</h3>
            <ul className={styles.calloutList}>
              {live.map((c, i) => (
                <CalloutItem key={`${c.subject}-${i}`} c={c} />
              ))}
              {lesson === "analytics" &&
                sites.map((s) => <SiteToday key={s.id} site={s} />)}
            </ul>
          </section>
        )}

        {l.body.map((p, i) => (
          <p key={i} className={styles.paragraph}>
            <Rich text={p} />
          </p>
        ))}

        {l.thresholds && (
          <p className={styles.rule}>
            <span className={styles.ruleLabel}>Assessment</span>{" "}
            <Rich text={l.thresholds} />
          </p>
        )}

        <div className={styles.columns}>
          {(
            [
              ["What normal looks like", l.normal],
              ["When to look closer", l.worry],
              ["What to do", l.act],
            ] as const
          ).map(([title, items]) => (
            <section key={title} className={styles.column} aria-label={title}>
              <h3 className={styles.blockTitle}>{title}</h3>
              <ul className={styles.points}>
                {items.map((item, i) => (
                  <li key={i}>
                    <Rich text={item} />
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </div>

        <div className={styles.pager}>
          {prev ? (
            <Button
              className={shared.button}
              onClick={() => onLessonChange(prev.id)}
            >
              ← {prev.title}
            </Button>
          ) : (
            <span />
          )}
          {next && (
            <Button
              className={shared.button}
              onClick={() => onLessonChange(next.id)}
            >
              {next.title} →
            </Button>
          )}
        </div>
      </article>
    </div>
  );
}

export default GuideView;
