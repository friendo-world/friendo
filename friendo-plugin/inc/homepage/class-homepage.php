<?php

namespace Friendo;

class Homepage
{

    public function init ()
    {
        $homepage = new self;
        $homepage->loadHomepage();
    }

    protected function loadHomepage () {
        add_action('init', function () {
            if ( $this->checkHomepage() === false ) {
                $this->setupHomepage();
            }
        });
    }

    protected function checkHomepage ()
    {
        $setup = get_option( 'show_on_front' ) === 'page' ? true : false;
        return $setup;
    }

    protected function setupHomepage ()
    {
        $homepageId = $this->createHomepage();

        if ($homepageId) {
            update_option( 'page_on_front', $homepageId );
            update_option( 'show_on_front', 'page' );
        }
    }

    public function createHomepage ()
    {
        $id = wp_insert_post([
            'post_title' => 'Hello Friendo World!',
            'post_type' => 'page',
            'post_status' => 'publish',
        ]);
        return $id;
    }
}