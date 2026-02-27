import type {ReactNode} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import Heading from '@theme/Heading';

import styles from './index.module.css';

function HomepageHeader() {
  const {siteConfig} = useDocusaurusContext();
  return (
    <header className={clsx('hero hero--primary', styles.heroBanner)}>
      <div className="container">
        <Heading as="h1" className="hero__title">
          {siteConfig.title}
        </Heading>
        <p className="hero__subtitle">{siteConfig.tagline}</p>
        <div className={styles.buttons}>
          <Link
            className="button button--secondary button--lg"
            to="/docs/getting-started">
            Get Started
          </Link>
          <Link
            className="button button--outline button--secondary button--lg"
            style={{marginLeft: '1rem'}}
            to="/docs/introduction">
            Learn More
          </Link>
        </div>
      </div>
    </header>
  );
}

type FeatureItem = {
  title: string;
  description: ReactNode;
};

const FeatureList: FeatureItem[] = [
  {
    title: 'Kubernetes-Native',
    description: (
      <>
        Isola extends Kubernetes with Custom Resource Definitions (CRDs) for
        sandbox lifecycle management. Create, monitor, and delete sandboxes
        using familiar Kubernetes patterns.
      </>
    ),
  },
  {
    title: 'Secure by Default',
    description: (
      <>
        Sandboxes run with gVisor for kernel-level isolation. Network traffic is
        deny-all by default with fine-grained egress controls. Environment
        variables are write-only to prevent secret leakage.
      </>
    ),
  },
  {
    title: 'Programmatic Control',
    description: (
      <>
        Execute commands, transfer files, and stream output through a REST API
        or the Python SDK. Both synchronous and asynchronous clients are
        supported with automatic reconnection.
      </>
    ),
  },
  {
    title: 'Filesystem Snapshots',
    description: (
      <>
        Capture point-in-time snapshots of sandbox filesystems and upload
        them to S3, GCS, or Azure Blob Storage. Supports TTL-based
        auto-cleanup and revision tracking.
      </>
    ),
  },
];

function Feature({title, description}: FeatureItem) {
  return (
    <div className={clsx('col col--6')} style={{marginBottom: '1.5rem'}}>
      <div className="feature-card">
        <Heading as="h3">{title}</Heading>
        <p>{description}</p>
      </div>
    </div>
  );
}

function HomepageFeatures(): ReactNode {
  return (
    <section className={styles.features}>
      <div className="container">
        <div className="row">
          {FeatureList.map((props, idx) => (
            <Feature key={idx} {...props} />
          ))}
        </div>
      </div>
    </section>
  );
}

export default function Home(): ReactNode {
  return (
    <Layout description="Secure sandbox orchestration for Kubernetes">
      <HomepageHeader />
      <main>
        <HomepageFeatures />
      </main>
    </Layout>
  );
}
