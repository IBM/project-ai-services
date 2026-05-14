export interface UseCase {
  id: string;
  title: string;
  description: string;
  creator: string;
  domain: string;
  architectures: string[];
  assets: string[];
  demo?: string;
  clientStories?: Array<{
    company: string;
    description?: string;
    url?: string;
  }>;
  partnerStories?: Array<{
    company: string;
    description?: string;
    url?: string;
    testimonial?: string;
  }>;
}
